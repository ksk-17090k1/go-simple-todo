# Go `net/http` でつくる本番寄り JSON API 入門

これは **Go の文法は分かるがサーバは書いたことがない人** に向けた、
`net/http`（標準ライブラリのみ、フレームワーク不使用）で
本番でも通用する形の JSON API を組み上げるマニュアルです。

読み終える頃には、次のような構成の Todo API が自分で書けるようになっています。

- `net/http` の `ServeMux`（Go 1.22+ の拡張ルーティング）でルーティング
- `encoding/json` で JSON 入出力
- ドメイン型と Store インターフェイスで永続化層を分離（in-memory 実装）
- `log/slog` による構造化アクセスログ
- panic recovery / request ID ミドルウェア
- `http.Server` のタイムアウト設定
- `signal.NotifyContext` + `Server.Shutdown` による graceful shutdown
- `net/http/httptest` でのハンドラテスト

## この教材の前提

- Go の**文法**（変数、関数、struct、interface、goroutine の存在、error の扱い方）は理解している。
- **Go 1.22 以上** がインストールされている（`go version` で確認）。
- ターミナルの基本操作、`curl` が使える。
- HTTP の基本的な用語（メソッド、ステータスコード、ヘッダ）は聞いたことがある。

## 扱わないこと

- HTML を返すサーバ・テンプレートエンジン（本教材は JSON API のみ）
- 認証・認可（JWT・セッションなど）
- 永続的なストレージ（RDBMS / KVS）— in-memory の Store 実装のみで、実装差し替え可能な設計にとどめる
- ルーティングフレームワーク（Gin, Echo, Chi など）
- OpenTelemetry・メトリクス
- HTTPS（TLS）終端 — 本番では前段のロードバランサや ingress が担う前提

## ディレクトリ構成

```
web_server/
├── README.md                        ← このファイル（目次）
├── 01-preparation.md                ← 準備
├── 02-minimal-server.md             ← 最小サーバ
├── 03-json-response.md              ← JSON レスポンス
├── 04-routing-and-request.md        ← ルーティングとリクエスト解析
├── 05-context.md                    ← context の基礎
├── 06-store.md                      ← ドメインとストレージ層
├── 07-handlers.md                   ← ハンドラの実装
├── 08-middleware.md                 ← ミドルウェア
├── 09-server-and-shutdown.md        ← サーバ設定と graceful shutdown
├── 10-testing.md                    ← テスト
├── 11-appendix.md                   ← 付録（本編で扱わなかったこと）
└── app/                             ← 完成コード（`go run .` で動かせる）
    ├── go.mod
    ├── main.go
    ├── todo.go
    ├── store.go
    ├── httpx.go
    ├── handler.go
    ├── routes.go
    ├── middleware.go
    ├── handler_test.go
    └── store_test.go
```

各章のコードスニペットは、最終的に `app/` の完成コードと一致します。
章の途中で手を止めて `app/` を眺めれば「最終形はこうなる」を先読みできます。

## 目次

| 章  | タイトル                              | 主な学び                                                                 |
|-----|---------------------------------------|--------------------------------------------------------------------------|
| 01  | [準備](./01-preparation.md)            | プロジェクト作成、`go.mod` の意味                                          |
| 02  | [最小の HTTP サーバ](./02-minimal-server.md) | `Handler` / `HandlerFunc` / `ServeMux`、`ListenAndServe`                 |
| 03  | [JSON レスポンス](./03-json-response.md) | `encoding/json`、`Content-Type`、エラーレスポンスの共通形                    |
| 04  | [ルーティングとリクエスト解析](./04-routing-and-request.md) | Go 1.22+ の `ServeMux`、`PathValue`、`json.Decoder`               |
| 05  | [context の基礎](./05-context.md)      | `context.Context` の伝播、キャンセル、`WithValue` の作法                      |
| 06  | [ドメインとストレージ層](./06-store.md)| `Todo` 型、`Store` interface、in-memory 実装、`sync.RWMutex`                |
| 07  | [ハンドラの実装](./07-handlers.md)     | `TodoHandler` 構造体で依存注入、CRUD 5 本                                       |
| 08  | [ミドルウェア](./08-middleware.md)     | ミドルウェアのパターン、request ID、`slog` アクセスログ、panic recovery       |
| 09  | [サーバ設定と graceful shutdown](./09-server-and-shutdown.md) | `http.Server` のタイムアウト、`slog` セットアップ、`Server.Shutdown`         |
| 10  | [テスト](./10-testing.md)              | `httptest.NewRequest` / `NewRecorder`、テーブル駆動テスト                    |
| 11  | [付録](./11-appendix.md)               | 本編で扱わなかったこと、次に学ぶべきトピック                                  |

## 完成コードを動かす

途中で「動くもの」を触りたくなったら、以下で起動できます。

```sh
cd web_server/app
go run .
```

別ターミナルから叩いてみます。

```sh
# ヘルスチェック
curl -sS http://localhost:8080/healthz
# → {"status":"ok"}

# Todo 作成
curl -sS -X POST http://localhost:8080/todos \
  -H 'Content-Type: application/json' \
  -d '{"title":"buy milk"}'
# → {"id":1,"title":"buy milk","done":false,"created_at":"..."}

# 一覧
curl -sS http://localhost:8080/todos
# → {"todos":[{"id":1, ... }]}

# 単体取得
curl -sS http://localhost:8080/todos/1

# 更新
curl -sS -X PUT http://localhost:8080/todos/1 \
  -H 'Content-Type: application/json' \
  -d '{"title":"buy oat milk","done":true}'

# 削除
curl -sS -X DELETE http://localhost:8080/todos/1 -i
# → HTTP/1.1 204 No Content
```

テストも走らせておくと安心です。

```sh
cd web_server/app
go test ./...
```

準備ができたら [01. 準備](./01-preparation.md) に進んでください。
