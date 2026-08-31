# 09. サーバ設定と graceful shutdown

## この章のゴール

- `http.ListenAndServe` ではなく **`http.Server` を明示的に組む**理由を理解する
- 主要な **タイムアウト** を設定する
- **`log/slog`** を JSON ハンドラでセットアップする
- **SIGTERM / SIGINT を捕まえて graceful shutdown** する `main.go` を完成させる

## 9.1 なぜ `http.ListenAndServe` では不足なのか

これまで書いてきた `http.ListenAndServe(":8080", mux)` は内部で [`http.Server`](https://pkg.go.dev/net/http#Server) を作りますが、**すべてゼロ値**です。
つまり:

- 読み書きの **タイムアウトなし** → slow-loris 攻撃・切れかけコネクションでリソース枯渇
- **graceful shutdown できない**（`Server.Shutdown` を呼ぶ手段がない）

本番向けには **`http.Server` を自分で組み立てる** のが定石です。

## 9.2 タイムアウトの意味

```go
srv := &http.Server{
    Addr:              ":8080",
    Handler:           h,
    ReadHeaderTimeout: 5 * time.Second,
    ReadTimeout:       10 * time.Second,
    WriteTimeout:      10 * time.Second,
    IdleTimeout:       120 * time.Second,
}
```

| フィールド            | 意味                                                                                        | 目安                |
|-----------------------|---------------------------------------------------------------------------------------------|---------------------|
| `ReadHeaderTimeout`   | クライアントがリクエストヘッダを送り終えるまでの最大時間                                       | 5s               |
| `ReadTimeout`         | ヘッダ + ボディを **すべて読み終える** までの最大時間                                          | 10s              |
| `WriteTimeout`        | ハンドラが応答を書き終えるまでの最大時間（レスポンスヘッダ送信開始からのカウント）              | 10s              |
| `IdleTimeout`         | keep-alive 接続で **次のリクエストを待つ** 最大時間                                            | 120s             |

とくに **`ReadHeaderTimeout`** は最重要です。これがゼロだと slow-loris（1 バイトずつヘッダを送り続ける攻撃）で接続が延々と滞留します。

WriteTimeout はストリーミングエンドポイントを持つと厳しくなるので、そういう API がある場合は個別に見直します。今回の Todo API は短時間で完結するので 10 秒で十分。

## 9.3 slog のセットアップ

```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelInfo,
}))
slog.SetDefault(logger)
```

### 何が起きるか

- **[`NewJSONHandler`](https://pkg.go.dev/log/slog#NewJSONHandler)** … 1 ログ = 1 行 JSON。ログ収集基盤（Datadog, Cloud Logging, Loki, ELK など）と相性がよい。
- **`os.Stdout`** に出す … 12-Factor App の「ログはイベントストリーム、出力先はプロセスの外」の原則に従う。
  ファイル書き出しはやらない（コンテナ環境では特に）。
- **`Level: slog.LevelInfo`** … Debug は落として Info 以上を出す。開発時は `LevelDebug` にする。
- **[`slog.SetDefault(logger)`](https://pkg.go.dev/log/slog#SetDefault)** … デフォルトの `slog.Info(...)` 等もこの logger 経由になる。他パッケージから `slog.Error("msg")` と書かれても同じ出力先に流れる。

### `log`（従来）との違い

- 従来の `log` は「1 メッセージ + オプションのフォーマット文字列」だけ。
- `slog` は **属性（key-value ペア）** を第一級で扱う。パース側で「status:500 のログだけ集計」といったクエリが簡単。

以降、本教材では **標準 `log` は使わず `slog` に統一** します。

## 9.4 graceful shutdown の考え方

サーバを止めるとき、**進行中のリクエストを途中で切りたくない**。
そこで次の手順を踏みます。

1. `SIGTERM`（コンテナランタイムからの停止指示）または `SIGINT`（Ctrl-C）を捕まえる
2. [`srv.Shutdown(ctx)`](https://pkg.go.dev/net/http#Server.Shutdown) を呼ぶ ─ 新規接続の受け入れを停止し、進行中のハンドラの終了を待つ
3. `ctx` にタイムアウトを付けておき、いつまでも待たないようにする

[`signal.NotifyContext`](https://pkg.go.dev/os/signal#NotifyContext) を使うと 1 の受信を **context のキャンセルとして** 受け取れます。

```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()
// ...
<-ctx.Done() // シグナルが来たらここが返る
```

## 9.5 完成版 `main.go`

`main.go` 全体を次の形にします。

```go
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// run は main の実処理。エラーを戻り値で返すことでテストしやすくなる
// （ここではテストしていないが、慣習として推奨されるパターン）。
func run() error {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	// 1) ログ: 構造化 JSON でプロセス標準出力に流す。
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// 2) 依存を組み立てる。
	store := NewMemStore()
	handler := NewTodoHandler(store, logger)

	// 3) ルーティング。
	mux := http.NewServeMux()
	registerRoutes(mux, handler)

	// 4) ミドルウェア。並びは「外→内」。
	//    requestID → logging → recovery → mux
	h := chain(mux,
		requestIDMiddleware,
		loggingMiddleware(logger),
		recoveryMiddleware(logger),
	)

	// 5) http.Server の設定。
	srv := &http.Server{
		Addr:              *addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// 6) SIGINT / SIGTERM を受けたら ctx がキャンセルされる。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 7) サーバは別 goroutine で走らせる。main は ctx.Done() を待つ。
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("server starting", "addr", *addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	// 8) シャットダウン待機。
	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	// 9) graceful shutdown: 進行中のリクエストを 10 秒だけ待つ。
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	logger.Info("server stopped cleanly")
	return nil
}
```

## 9.6 各ブロックの意図

### `run() error` に切り出す

`main()` は「エラーが返ったら 1 で exit する」だけの薄い皮にします。
実処理を `run() error` に切り出すことで:

- 途中の `os.Exit(1)` が散らばらず、`defer` が確実に走る
- テストしたければ `run` を呼べる（今回はやらない）
- エラーハンドリングが 1 箇所に集約される

### `errors.Is(err, http.ErrServerClosed)`

`Server.Shutdown` を呼ぶと、動いている `ListenAndServe` が **[`http.ErrServerClosed`](https://pkg.go.dev/net/http#ErrServerClosed)** を返します。
これは正常終了の合図なので、エラーとして扱ってはいけません。**`errors.Is` で判定** します。

### なぜ `select` で 2 系統を待つのか

サーバは 2 通りの止まり方があります。

- シャットダウンシグナル（`ctx.Done()`）
- リッスン開始そのものが失敗（`ListenAndServe` が即エラーで返る）

どちらが先に起きるか分からないので、`select` で両方を待ちます。
片方に絞ってしまうと、ポート衝突などのケースで「起動失敗しているのに main は永遠に待機」といったハングを起こします。

### `signal.NotifyContext` は何をやっているか

- [`signal.Notify(chan, ...)`](https://pkg.go.dev/os/signal#Notify) の context 版
- 内部で「シグナルを受けたら context をキャンセル」してくれる
- `defer stop()` を必ず入れる（プロセス内で複数箇所からシグナルを扱う場合の綺麗な後始末）

### shutdown のタイムアウト

[`context.WithTimeout(..., 10*time.Second)`](https://pkg.go.dev/context#WithTimeout) は「進行中のリクエストを 10 秒だけ待つ」の意味です。
それを超えると `Shutdown` は `context.DeadlineExceeded` を返し、まだ処理中の接続は強制切断されます。
本番では **k8s の terminationGracePeriodSeconds よりわずかに短い** 値にするのが定石（例: k8s が 30s なら 25s）。

## 9.7 動作確認

```sh
cd web_server/app
go run .
# {"time":"...","level":"INFO","msg":"server starting","addr":":8080"}
```

別ターミナルから叩く:

```sh
curl -sS http://localhost:8080/healthz
# {"status":"ok"}
```

Ctrl-C で止めると:

```
{"time":"...","level":"INFO","msg":"shutdown signal received"}
{"time":"...","level":"INFO","msg":"server stopped cleanly"}
```

これで **プロダクション寄りの Todo API** が動く形で完成しました。
残るは「壊れないように **テスト** をかける」だけです。

## 次の章

[10. テスト →](./10-testing.md)
