# 04. ルーティングとリクエスト解析

## この章のゴール

- Go 1.22 で入った `ServeMux` の **拡張ルーティング** を使いこなす
- パスパラメータ・クエリ・JSON ボディの解析パターンを身につける
- JSON ボディの安全な読み方（サイズ制限・未知フィールド拒否）を学ぶ

## 4.1 Go 1.22 の拡張 `ServeMux`

Go 1.22 で `net/http.ServeMux` に **メソッド指定** と **パスパラメータ** が入りました。
これで多くの入門ケースは外部ルータ（Chi・Gorilla mux 等）が不要になります。

### 4.1.1 メソッド + パス

```go
mux.HandleFunc("GET /todos", listHandler)
mux.HandleFunc("POST /todos", createHandler)
mux.HandleFunc("GET /todos/{id}", getHandler)
mux.HandleFunc("PUT /todos/{id}", updateHandler)
mux.HandleFunc("DELETE /todos/{id}", deleteHandler)
```

パターンの左が **HTTP メソッド**、右が **パス** です。両者は 1 個の半角スペースで区切ります。
「同じパスに複数メソッド」を登録すると、マッチしないメソッドに対して自動で **`405 Method Not Allowed`** が返ります。

### 4.1.2 パスパラメータ

`{name}` で名前付きプレースホルダを書けます。ハンドラ内では `r.PathValue("name")` で取り出します。

```go
mux.HandleFunc("GET /todos/{id}", func(w http.ResponseWriter, r *http.Request) {
    raw := r.PathValue("id") // 文字列
    // ...
})
```

- `{name...}` にするとパスの残り全体（`/foo/{path...}`）にマッチします。今回は使いません。
- 型変換は自分でやる必要があります（`strconv.ParseInt` など）。

### 4.1.3 なぜ「フレームワーク不要」と言えるようになったか

Go 1.21 以前の `ServeMux` は「メソッド指定できない」「パスパラメータない」ため、
実質どのプロジェクトも Chi や Gorilla mux を入れていました。
1.22 の拡張でこの 2 つが標準に入ったので、**外部ルータは不要**になったケースが多いです。

## 4.2 パスパラメータを int に変換するヘルパー

`handler.go` を作って以下のヘルパーを入れます（今後ここに CRUD ハンドラを追加していきます）。

```go
// handler.go
package main

import (
	"net/http"
	"strconv"
)

// parsePathID は URL パスの {id} を int64 として取り出す。
// 不正なら 400 を返しつつ ok=false を返す。
func parsePathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_path", "id must be a positive integer")
		return 0, false
	}
	return id, true
}
```

呼び出し側:

```go
id, ok := parsePathID(w, r)
if !ok {
    return // レスポンスは書き終わっている
}
// id を使って処理
```

「エラー時はレスポンス書いて `false` を返す」パターンは Go の慣習的な hack ですが、
ハンドラ本体が読みやすくなるので採用しています。

## 4.3 クエリパラメータ

`?done=true` のようなクエリは `r.URL.Query()` で取り出します。

```go
q := r.URL.Query()
onlyDone := q.Get("done") == "true"
limit, _ := strconv.Atoi(q.Get("limit"))
```

- `q.Get("x")` は存在しなければ空文字列。
- 同じキーが複数値ある場合（`?tag=a&tag=b`）は `q["tag"]` で `[]string` を取得。
- 型変換は自分でやる。デフォルト値も自分で決める。

本教材の Todo API ではクエリは使いませんが、フィルタリングを足すときは上のパターンで済みます。

## 4.4 JSON ボディを読む

`POST /todos` や `PUT /todos/{id}` は JSON のリクエストボディを受け取ります。

### 4.4.1 最小の書き方（あまり良くない例）

```go
var req struct {
    Title string `json:"title"`
}
if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
    writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
    return
}
```

これでも動きますが、次の 2 つが抜けています。

- **ボディサイズの上限がない** → 攻撃者が 10GB の JSON を送ってきたらメモリ食い潰す
- **未知フィールドを黙って許容してしまう** → クライアントのタイプミス（`titel` など）に気付けない

### 4.4.2 本番向けの書き方

```go
const maxBodyBytes = 1 << 20 // 1 MiB

r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
dec := json.NewDecoder(r.Body)
dec.DisallowUnknownFields()
if err := dec.Decode(&req); err != nil {
    writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
    return
}
if dec.More() {
    writeError(w, http.StatusBadRequest, "invalid_body", "body must contain a single JSON object")
    return
}
```

3 つのガードが入りました。

- `http.MaxBytesReader` … `n` バイト超えると `Decode` がエラーを返す
- `dec.DisallowUnknownFields()` … 未知フィールドがあると `Decode` がエラーを返す
- `dec.More()` … `{...}` の後ろに追加の JSON がぶら下がっていたら弾く

### 4.4.3 ジェネリクスで共通化する

同じ 10 行を毎ハンドラで書くと DRY に反するので、ジェネリクスでヘルパーにします。

```go
// handler.go に追記
import "encoding/json"

const maxBodyBytes = 1 << 20

func decodeJSON[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var v T
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return v, false
	}
	if dec.More() {
		writeError(w, http.StatusBadRequest, "invalid_body", "body must contain a single JSON object")
		return v, false
	}
	return v, true
}
```

呼び出し側:

```go
type createTodoReq struct {
    Title string `json:"title"`
}

req, ok := decodeJSON[createTodoReq](w, r)
if !ok {
    return
}
// req.Title を使う
```

## 4.5 まとめ

パス・クエリ・ボディの解析は本教材では次の 3 パターンに集約されます。

| 何を        | どう取り出す                                   | エラー時のステータス |
|-------------|------------------------------------------------|----------------------|
| パス `{id}` | `parsePathID(w, r)`                            | `400 invalid_path`   |
| クエリ      | `r.URL.Query().Get(...)`                       | ハンドラで判断        |
| JSON ボディ | `decodeJSON[T](w, r)`                          | `400 invalid_body`   |

ここまでで**「リクエストを受け取り JSON を返す」道具は揃いました**。
ただ、まだ大事な要素が残っています ── **`context`** です。次章で扱います。

## 次の章

[05. context の基礎 →](./05-context.md)
