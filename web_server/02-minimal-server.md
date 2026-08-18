# 02. 最小の HTTP サーバ

## この章のゴール

- `net/http` の中心概念（`Handler` / `HandlerFunc` / `ServeMux`）を理解する
- 8 行のサーバを書いて起動する
- `curl` で叩いて動作を確認する

## 2.1 net/http の登場人物

`net/http` のサーバサイドで押さえるべき型はたった 3 つです。

### 2.1.1 `http.Handler` インターフェイス

```go
type Handler interface {
    ServeHTTP(w http.ResponseWriter, r *http.Request)
}
```

「HTTP リクエストを 1 本処理できるモノ」を表す最小のインターフェイス。
`ServeHTTP` は「レスポンス書き先（`w`）」と「リクエスト（`r`）」を受け取ります。

### 2.1.2 `http.HandlerFunc`

`Handler` を毎回 struct にするのは面倒なので、**関数を Handler に見せる型** が用意されています。

```go
type HandlerFunc func(w http.ResponseWriter, r *http.Request)

func (f HandlerFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    f(w, r)
}
```

普通の関数を `http.HandlerFunc(myFunc)` にキャストするだけで `Handler` として扱えるようになります。

### 2.1.3 `http.ServeMux`

URL パス（と Go 1.22+ ではメソッドも）を Handler にマッピングする **ルータ** です。
リクエストが来ると、パターンに一致した Handler の `ServeHTTP` を呼びます。

## 2.2 8 行のサーバ

まず `main.go` を作って以下を書きます。

```go
// main.go
package main

import (
	"fmt"
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /hello", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "hello")
	})
	_ = http.ListenAndServe(":8080", mux)
}
```

起動と確認:

```sh
go run .
# 別ターミナルで
curl -sS http://localhost:8080/hello
# → hello
```

止めるときは Ctrl-C。

## 2.3 コードのポイント

### `http.NewServeMux()`

明示的に新しい `ServeMux` を作っています。「`nil` を渡すとデフォルトの `DefaultServeMux` が使われる」という慣習もありますが、
**プロダクションでは常に自分で作った `ServeMux` を渡す** べきです。理由は次の 2 つ:

1. 依存パッケージが勝手に `http.HandleFunc(...)` で `DefaultServeMux` に登録すると、意図しないパスが公開される。
2. テストで別の mux を使いたい時、グローバル状態を触りたくない。

### `mux.HandleFunc("GET /hello", ...)`

Go 1.22 以上では、パターンに **メソッド指定** を書けます。
`"GET /hello"` にすると GET だけがマッチし、POST 等は自動で **`405 Method Not Allowed`** が返ります。
（1.22 未満だとメソッドを書けないので `if r.Method != http.MethodGet { ... }` を各ハンドラで書く必要がありました。もう不要です。）

### `http.ListenAndServe(":8080", mux)`

`:8080` で待ち受け、リクエストを `mux` に流します。第 2 引数を `nil` にすると `DefaultServeMux` が使われます（前述の通り避ける）。

戻り値のエラーを `_` で捨てていますが、**これは後で直します**（章 09）。
今はまだ「最短で動く形」を優先しています。

## 2.4 よくある落とし穴

### ヘッダを書いた後にステータスは変えられない

`http.ResponseWriter` は次のように使います。

```go
w.Header().Set("X-Custom", "value")   // 1. ヘッダをセット（この時点ではまだ送信されない）
w.WriteHeader(http.StatusCreated)     // 2. ステータスコードを確定（ここでヘッダが送信される）
fmt.Fprintln(w, "body")               // 3. ボディを書く
```

順番を間違えると混乱の元です。特に **`w.Write` / `fmt.Fprintln` の呼び出しは、暗黙的に `WriteHeader(200)` を発火します**。
その後で `WriteHeader(500)` を呼んでも「200 で 500 の本文を返す」というちぐはぐな挙動になります。

### 「レスポンスが返ってこない」時に見るべき点

- ハンドラ内で長時間ブロックしていないか
- ハンドラの前で早期リターンしていて、`w` に何も書いていない → クライアントは `200 空ボディ` を受け取る
- パスが違う → デフォルトの ServeMux は `404` を JSON ではなく `text/plain` で返す（章 03 で JSON に統一します）

## 次の章

[03. JSON レスポンス →](./03-json-response.md)
