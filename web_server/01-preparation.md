# 01. 準備

## この章のゴール

- 作業ディレクトリと Go モジュールを作成する
- `go.mod` が何をしているのかを理解する
- これから触るファイル一覧を頭に入れる

## 1.1 Go のバージョンを確認する

本教材は **Go 1.22 以上** を前提にします。`ServeMux` の拡張ルーティング（後述）が [Go 1.22](https://go.dev/doc/go1.22) で入ったためです。

```sh
go version
# 例: go version go1.23.4 darwin/arm64
```

`1.22` より前だった場合は、[公式のダウンロードページ](https://go.dev/dl/) から更新してください。

## 1.2 プロジェクトディレクトリと `go.mod`

新しい API を書くときは、まずディレクトリを作って `go mod init` で **[モジュール](https://go.dev/ref/mod)** を作ります。
モジュールは Go における依存関係の単位で、`go.mod` がその宣言ファイルです。

```sh
mkdir -p todoapi
cd todoapi
go mod init todoapi
```

生成される `go.mod`:

```
module todoapi

go 1.22
```

- `module todoapi` はこのモジュールのパス。他のモジュールから `import "todoapi/xxx"` する時のプレフィックス。
- `go 1.22` はこのモジュールが要求する最低 Go バージョン。ここが 1.22 以上でないと後で使う `ServeMux` の拡張パターンが動きません。

> **今回は外部依存なし** です。標準ライブラリだけで書き切ります。したがって `go.sum` も生成されません。

## 1.3 これから作るファイル

最終的に以下のようなファイル構成になります。今の時点で作る必要はありません。章を進めるごとに 1 つずつ足していきます。

| ファイル              | 役割                                                              | 登場する章 |
|-----------------------|-------------------------------------------------------------------|------------|
| `main.go`             | エントリポイント。全部品を組み立てて `http.Server` を起動する      | 02, 09     |
| `todo.go`             | ドメイン型 `Todo` の定義                                           | 06         |
| `store.go`            | `Store` インターフェイスと in-memory 実装                          | 06         |
| `httpx.go`            | `writeJSON` / `writeError` / context ヘルパー                     | 03, 05     |
| `handler.go`          | `TodoHandler` 構造体と CRUD メソッド                                   | 07         |
| `routes.go`           | ルーティング登録                                                   | 04, 07     |
| `middleware.go`       | request ID / logging / recovery ミドルウェア                       | 08         |
| `handler_test.go`     | `httptest` を使ったハンドラテスト                                  | 10         |
| `store_test.go`       | Store の単体テスト                                                 | 10         |

すべて `package main` に属する平置き構成にします。実際のプロジェクトではドメインごとに `internal/xxx` パッケージに切ることも多いですが、教材としての読みやすさを優先しています（[付録](./11-appendix.md) で触れます）。

## 次の章

[02. 最小の HTTP サーバ →](./02-minimal-server.md)
