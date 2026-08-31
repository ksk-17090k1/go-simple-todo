# 11. 付録

本編で扱わなかったトピックを、**「なぜ扱わなかったか」と「次に学ぶならどこから」**の観点で紹介します。

## 11.1 認証・認可

- **なぜ扱わなかったか**: 認証は JWT / セッションクッキー / OIDC / mTLS など選択肢が多く、設計が API 本体より重い。入門から外した。
- **やり方の概要**: 認証ミドルウェアで `Authorization` ヘッダを検証 → ユーザ情報を context に載せる → ハンドラで `UserFromContext(r.Context())` で取り出す。
  [章 05](./05-context.md) と [章 08](./08-middleware.md) の組み合わせで自然に書ける。
- **参考**: JWT なら [`golang-jwt/jwt`](https://github.com/golang-jwt/jwt)、OIDC なら [`coreos/go-oidc`](https://github.com/coreos/go-oidc)。

## 11.2 永続化 (DB)

- **なぜ扱わなかったか**: SQL・マイグレーション・接続プール・トランザクションと、扱う話が多岐にわたる。net/http の学習から逸れる。
- **やり方の概要**: `Store` インターフェイスの別実装として `SQLStore` を作る。標準の `database/sql` + ドライバ（`jackc/pgx/v5/stdlib` など）。
  `db.QueryContext(ctx, ...)` で context を活かす。
- **移行手順**: ハンドラは 1 行も変更不要。`main.go` の

  ```go
  store := NewMemStore()
  ```

  を

  ```go
  store, err := NewSQLStore(dsn)
  ```

  に差し替えるだけ。ここが `Store` interface を最初から入れた **投資回収** の瞬間。

## 11.3 リクエストバリデーション

本編では「必須チェック」だけ手書きで済ませました。要件が増えたら:

- 手書き検証を関数に切り出す（依存を増やさず柔軟）
- [`go-playground/validator`](https://github.com/go-playground/validator) を導入する（タグベース、豊富なルール）

初期はタグベースの魅力に負けがちですが、複雑になると手書きに戻る現場も多いです。**まず手書きで、辛くなったら導入** を推奨。

## 11.4 メトリクス・トレース (OpenTelemetry)

- **やり方の概要**: OpenTelemetry の Go SDK と `otelhttp` パッケージ。
  `otelhttp.NewHandler(mux, "todoapi")` でラップするだけで span を発行できる。
- 本教材の request ID ミドルウェアは、OpenTelemetry を入れたら不要になる（trace ID を使う）。

## 11.5 HTTPS (TLS 終端)

- 本教材では **HTTPS を扱っていません**。
- 現実世界のプロダクションでは **ロードバランサや ingress で TLS を終端** し、backend の Go サーバは平文 HTTP で動かす構成が主流です。Let's Encrypt を自前でハンドリングする必要はまずない。
- Go 単体で HTTPS を喋りたい場合は [`srv.ListenAndServeTLS(certFile, keyFile)`](https://pkg.go.dev/net/http#Server.ListenAndServeTLS)。

## 11.6 パッケージ分割 (`internal/`)

本教材は **平置き（すべて `package main`）** です。実プロジェクトでは:

```
todoapi/
├── cmd/todoapi/main.go
└── internal/
    ├── todo/       (Todo 型, Store, MemStore)
    ├── httpx/      (writeJSON, ctx helpers)
    └── server/     (TodoHandler, routes, middleware)
```

のように切ることが多いです。[`internal/`](https://pkg.go.dev/cmd/go#hdr-Internal_Directories) に置くと **モジュール外部から import できない** ので、公開 API を絞る仕組みとして機能します。

コード量が数千行を超えたら分割検討、それ未満ならフラットで OK、が経験則。

## 11.7 `encoding/json/v2` （実験機能）

- Go 1.24 以降、`GOEXPERIMENT=jsonv2` で `encoding/json/v2` が試せる。
- API はより **明示的**（`json.Marshal` のオプション、pluggable encoder）。
- **執筆時点（2026-08）ではまだ実験的**。API が変わる可能性があるので、プロダクションでは `encoding/json` を使う。

## 11.8 フレームワーク比較（参考程度に）

本教材はフレームワーク不使用ですが、規模と要件によっては選ぶ価値があります。

| ライブラリ    | 立ち位置                                              | 使いどころ                                      |
|---------------|-------------------------------------------------------|-------------------------------------------------|
| **標準 `net/http` (本教材)** | ゼロ依存。Go 1.22+ の ServeMux で入門〜中規模まで対応 | 依存を最小にしたい。中小規模。                    |
| **chi**       | 薄いルータ + ミドルウェア群。`net/http` と互換         | ルータの表現力（サブルーティング等）が欲しい     |
| **gin**       | 高機能・独自エコシステム                              | 素早く書きたい。標準ハンドラとの互換性は捨てる    |
| **echo**      | gin と似た立ち位置                                    | gin と好みで                                    |
| **fiber**     | fasthttp ベース（`net/http` 非互換）                  | 極端な性能が要る場合                            |

`net/http` の中身を理解した後で選ぶと、どのフレームワークもインタフェースの意図が読めるようになります。

## 11.9 HTTP クライアント側

本教材はサーバ側ですが、Go でクライアントを書くときの原則を短く。

```go
client := &http.Client{Timeout: 5 * time.Second}
req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
resp, err := client.Do(req)
if err != nil { ... }
defer resp.Body.Close()
```

**必ず [`http.NewRequestWithContext`](https://pkg.go.dev/net/http#NewRequestWithContext)** を使い、[`http.Client`](https://pkg.go.dev/net/http#Client) に **タイムアウト** を設定する。
`resp.Body` は必ず `Close` する（`defer` すぐ）。
[`http.Get(...)`](https://pkg.go.dev/net/http#Get) のようなグローバル関数は timeout がないので **本番では使わない**。

## 11.10 参考リンク

- Go 公式ドキュメント: <https://pkg.go.dev/net/http>
- `log/slog` ドキュメント: <https://pkg.go.dev/log/slog>
- ServeMux 拡張の設計文書 (proposal #61410): <https://github.com/golang/go/issues/61410>

## お疲れさまでした

`net/http` だけでも本番相当のサーバは組めることが体感できたはずです。
次のステップは、**この Todo API を書き換えて自分のユースケースに寄せていく** ことです。

- `Store` を SQLite なり Postgres なりに差し替える
- `Todo` を自分のドメイン型に変える
- 認証を足す

いずれも **既存のハンドラや `main.go` を最低限しか触らずに** できるはずです。
それがこの教材で狙った設計です。楽しんでください。
