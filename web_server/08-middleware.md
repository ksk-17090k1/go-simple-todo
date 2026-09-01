# 08. ミドルウェア

## この章のゴール

- ミドルウェアの型（`func(http.Handler) http.Handler`）を理解する
- 複数のミドルウェアを合成する `chain` を書く
- 3 種の実用ミドルウェアを実装する: **request ID / logging / recovery**
- ミドルウェアの **並び順の意味** を把握する

## 8.1 ミドルウェアとは

ミドルウェアは **「ハンドラをラップして共通処理を差し込む関数」** です。
「全リクエストに X-Request-ID を付けたい」「全リクエストをログしたい」「panic を拾いたい」といった横断的関心事を、
各ハンドラを直接書き換えずに実装する仕組みです。

型としては次の 1 行で表せます。

```go
type Middleware func(http.Handler) http.Handler
```

「Handler を受け取って Handler を返す関数」。この統一型のおかげで、いくつでも重ねられます。

## 8.2 合成関数 `chain`

`middleware.go` を作ります。

```go
// middleware.go
package main

import "net/http"

type Middleware func(http.Handler) http.Handler

// chain は複数のミドルウェアを合成する。
// 呼び出し順は「引数の並び順にリクエストが通る」＝先頭が最外側。
//
//   chain(mux, A, B, C)  →  A(B(C(mux)))
func chain(h http.Handler, mws ...Middleware) http.Handler {
	// 逆順に適用すると、先頭のミドルウェアが最外側になる。
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}
```

**なぜ「先頭が最外側」にするか**: リクエストが `A → B → C → 最終ハンドラ` の順に流れる、という直感に一致するからです。

## 8.3 Request ID ミドルウェア

各リクエストに **一意な ID** を振り、ログにもレスポンスヘッダにも同じ ID を載せます。
「本番でユーザーから 500 の報告が来た → ID から該当ログを引く」の流れを支える基本装備です。

```go
import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"
)

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		ctx := withRequestID(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand の失敗は環境レベルの異常。時刻ベースにフォールバック。
		return time.Now().UTC().Format("20060102T150405.000000000")
	}
	return hex.EncodeToString(b[:])
}
```

### ポイント

- **クライアントが `X-Request-ID` を送ってきていれば尊重する**。トレーシング基盤（front proxy）が上流で振ることが多いため。
- 無ければ 16 桁の hex（8 バイト乱数）を生成。衝突は事実上ない。
- `withRequestID` は [章 05](./05-context.md) で書いた context ヘルパー。

## 8.4 statusRecorder

ロギング・リカバリの前に、両方で使う小さな道具を作ります。
標準の `http.ResponseWriter` は **書き込まれたステータスコード** を後から読めません。
アクセスログに「このリクエストの応答は 404 でした」を書くには、ステータスを **記録する ResponseWriter** が要ります。

```go
type statusRecorder struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	r.status = status
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		// 明示的な WriteHeader なしに Write されたら 200 として扱う。
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}
```

`ResponseWriter` を **埋め込み** ているので、`Header()` などは自動で下位に委譲されます。
`WriteHeader` と `Write` だけ **オーバーライド** して統計を取ります。

## 8.5 Logging ミドルウェア（slog アクセスログ）

[`log/slog`](https://pkg.go.dev/log/slog) で構造化ログを出します。1 リクエスト＝ 1 行 JSON。

```go
import "log/slog"

func loggingMiddleware(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			logger.InfoContext(r.Context(), "http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"bytes", rec.bytes,
				"duration", time.Since(start),
				"remote", r.RemoteAddr,
				"request_id", RequestIDFromContext(r.Context()),
			)
		})
	}
}
```

### ポイント

- **[`InfoContext`](https://pkg.go.dev/log/slog#Logger.InfoContext)** を使う。`Info(...)` と違い `context.Context` を渡せるので、リクエストスコープの情報（トレース ID 等）をログハンドラが利用できる。
- 属性は `"key", value` の **交互ペア** で渡す。数が合わない書き間違いは `go vet` が検出してくれる。
- より厳密・低アロケーションな **[`LogAttrs`](https://pkg.go.dev/log/slog#Logger.LogAttrs)**（`slog.String` 等の型付き [`Attr`](https://pkg.go.dev/log/slog#Attr) を渡す）もある。ホットパスや共通ヘルパーではこちらを選ぶチームも多い。
- 出力先は次章（09）で `slog.NewJSONHandler(os.Stdout, ...)` として設定する。
- `duration` は `time.Duration`。slog では nanoseconds 単位で JSON に出る。

## 8.6 Recovery ミドルウェア

ハンドラが `panic` すると `net/http` 標準では次のように振る舞います:

- panic はサーバゴルーチンに伝播しないよう捕捉される（プロセスは死なない）
- しかし **ログはスタックトレースだけ標準出力に出る**（構造化されていない）
- クライアントには **何も返されない**（コネクションが切られる）

これでは運用に耐えないので、自前で拾って **500 の JSON エラー** を返します。

```go
import "runtime/debug"

func recoveryMiddleware(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.ErrorContext(r.Context(), "panic recovered",
						"panic", rec,
						"stack", string(debug.Stack()),
						"request_id", RequestIDFromContext(r.Context()),
					)
					writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
```

### `any` 型の値をそのまま渡せる理由

`rec`（`recover()` の返り値）は `any` 型。軽い記法では `"panic", rec` とそのまま渡せば、
`slog` 内部で [`slog.Any`](https://pkg.go.dev/log/slog#Any) 相当として扱われ、
デフォルトの整形（`%v` 相当）で JSON に載せてくれます。

## 8.7 並び順の意味

`main.go` では次のように組み立てます（本編では 09 章で完成させます）。

```go
h := chain(mux,
    requestIDMiddleware,
    loggingMiddleware(logger),
    recoveryMiddleware(logger),
)
```

適用結果は `requestID( logging( recovery( mux )))` です。
リクエストは **外→内** に流れます:

```
[request in]
 ↓ requestID: ctx に ID を載せる
 ↓ logging: 時刻を記録・ResponseWriter をラップ
 ↓ recovery: defer で panic を捕捉
 ↓ mux: 該当ハンドラへ
[handler]
 ↑ 戻り値なし。書き込み済み ResponseWriter を持ち帰る
 ↑ recovery: panic があれば拾って 500 を書く
 ↑ logging: 実行時間・ステータスをログに残す ← ここが「defer で常に走る」わけではない
```

### 「logging を最外側 vs 最内側」トレードオフ

- **logging を recovery の外側** に置くと、**panic が起きても** recovery が 500 を書いた後にログが残る（今回の並び）
- **logging を recovery の内側** に置くと、panic 時にログ関数まで到達せずアクセスログが欠落する

したがって **logging は recovery より外側** が正解です。

### request ID を最外側にする理由

recovery のログにも request ID を含めたいからです。
request ID が recovery の内側だと、recovery のログには ID が乗りません（context にまだ無いため）。

## 8.8 ここまでで揃ったこと

- リクエストごとに **一意な ID**
- 全リクエストの **構造化アクセスログ**
- panic の **確実な捕捉と 500 レスポンス**

残るは「サーバをきちんと起動 / 停止する」だけです。次章で仕上げます。

## 次の章

[09. サーバ設定と graceful shutdown →](./09-server-and-shutdown.md)
