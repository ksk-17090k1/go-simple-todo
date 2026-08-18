# 03. JSON レスポンス

## この章のゴール

- [`encoding/json`](https://pkg.go.dev/encoding/json) で構造体を JSON に変換する
- 正しい `Content-Type` とステータスコードを返す
- **エラーレスポンスの形式** を最初に決める
- あちこちで再利用する `writeJSON` / `writeError` ヘルパーを作る

## 3.1 なぜ最初にヘルパーを作るのか

JSON を返すだけなら `json.NewEncoder(w).Encode(v)` の 1 行で終わりです。しかし本番のサーバでは：

- **Content-Type を毎回セットするのを忘れる**（クライアント側でパースに失敗する）
- **成功時と失敗時でボディ形式がバラバラ**（クライアント実装がつらい）
- **エラー時のフォーマットを後から統一しようとすると全ハンドラを直す羽目になる**

この 3 点を最初に潰しておくと、後の章がぐっと楽になります。

## 3.2 レスポンスの契約を決める

本教材では次の形式で統一します。

### 成功レスポンス

そのままドメイン型 or ラッパを返します。

```json
// GET /todos/1
{ "id": 1, "title": "buy milk", "done": false, "created_at": "2026-08-18T09:00:00Z" }

// GET /todos
{ "todos": [ ... ] }
```

一覧のとき配列を裸で返すか `{"todos": [...]}` で包むかは好み分かれるところですが、
**将来ページング情報を足したくなる**（`{"todos": [...], "next": "..."}` にしたい）ので、
最初からオブジェクトで包む方針にします。

### エラーレスポンス

エラーは常に同じ形にします。

```json
{ "error": { "code": "not_found", "message": "todo not found" } }
```

`code` は機械可読、`message` は人間向け。クライアントは `code` を見て分岐すればよくなります。

## 3.3 `writeJSON` を書く

`httpx.go` を作って以下を貼ります。

```go
// httpx.go
package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// writeJSON はハンドラから JSON レスポンスを書き出す共通処理。
// 1) Content-Type を先にセット、2) ステータスコードを書き、3) 本体を書く。
// この順序を守らないと Content-Type が意図した値にならないことがある。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// ヘッダは既に flush 済みなのでクライアントに返せる手段はない。ログだけ残す。
		slog.Error("failed to encode json response", "err", err)
	}
}
```

### [`Marshal`](https://pkg.go.dev/encoding/json#Marshal) vs [`NewEncoder(w).Encode`](https://pkg.go.dev/encoding/json#NewEncoder)

`json.Marshal` は `[]byte` を返します。`w.Write(b)` する 2 段構え。
`json.NewEncoder(w).Encode(v)` は直接 `w` に書きます。1 回のバッファリングで済むので **後者を推奨** します。
（副作用として `Encode` は末尾に `\n` を足しますが、これは気にしなくて OK。多くのクライアントは受け入れます。）

### `Content-Type` の値

`application/json` だけでもよいですが、`charset=utf-8` を付けておくと古いクライアントで文字化けを防げます。

## 3.4 `writeError` を書く

同じ `httpx.go` に追記します。

```go
// ErrorResponse はエラー返却の共通フォーマット。
// クライアントは常に { "error": { "code": ..., "message": ... } } を期待できる。
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, ErrorResponse{Error: ErrorBody{Code: code, Message: message}})
}
```

## 3.5 動作確認する

`main.go` を書き換えて、JSON を返すエンドポイントに変えます。

```go
package main

import "net/http"

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /hello", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"message": "hello"})
	})
	mux.HandleFunc("GET /boom", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusTeapot, "im_a_teapot", "short and stout")
	})
	_ = http.ListenAndServe(":8080", mux)
}
```

```sh
go run .

curl -sS -i http://localhost:8080/hello
# HTTP/1.1 200 OK
# Content-Type: application/json; charset=utf-8
# ...
# {"message":"hello"}

curl -sS -i http://localhost:8080/boom
# HTTP/1.1 418 I'm a teapot
# Content-Type: application/json; charset=utf-8
# ...
# {"error":{"code":"im_a_teapot","message":"short and stout"}}
```

## 3.6 補足: `encoding/json/v2` について

Go 1.24 以降、[`encoding/json/v2`](https://pkg.go.dev/encoding/json/v2) パッケージが `GOEXPERIMENT=jsonv2` の下で試せます。
API はより明示的で高速になっていますが、**執筆時点（2026-08）ではまだ実験的**で、
本番投入は時期尚早です。本教材では従来の `encoding/json` を使います。

## 次の章

[04. ルーティングとリクエスト解析 →](./04-routing-and-request.md)
