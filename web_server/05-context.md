# 05. context の基礎

## この章のゴール

- `context.Context` が何のためにあるかを掴む
- `r.Context()` の使い所（キャンセル伝播）を理解する
- `context.WithValue` の作法（typed key）を知る
- 以降の章で当たり前のように使う準備を整える

`context` は net/http の話ではなく Go 全体の話ですが、
「サーバでどう使うか」に絞って必要最低限だけ触れます。

## 5.1 なぜ `context` があるのか

サーバ側のプログラムでは、次のような状況が普通に起きます。

- **クライアントが接続を切った**のに、サーバ側は DB クエリを走らせ続けている
- **リクエストごとのタイムアウト**（例: 3 秒以内に応答）を守りたい
- **リクエストに紐づく ID や認証情報** を関数呼び出しの深いところまで運びたい

これらを **[`context.Context`](https://pkg.go.dev/context#Context)** という 1 個の値でまとめて表現するのが Go の流儀です。
`context.Context` は次の 2 つの側面を持ちます。

1. **キャンセルシグナル** — `ctx.Done()` を監視することで、「もう仕事をやめていい」を検知できる
2. **リクエストスコープの値** — `context.WithValue(parent, key, value)` で子 context に値を載せられる

## 5.2 net/http との関係

`*http.Request` は **必ず 1 個の `context.Context`** を持っています。

```go
ctx := r.Context()
```

この `ctx` は次の 2 つの状況で **自動的にキャンセル** されます。

- クライアントが接続を切ったとき
- サーバがシャットダウン中で、猶予時間内にリクエストが終わらなかったとき

つまり **[`r.Context()`](https://pkg.go.dev/net/http#Request.Context) を下流に渡しておくだけで**、クライアント切断時に DB クエリが自動でキャンセルされる、といった挙動を無料で手に入れられます。
（DB ドライバが `context` を尊重する実装になっている前提。[`database/sql`](https://pkg.go.dev/database/sql) の [`QueryContext`](https://pkg.go.dev/database/sql#DB.QueryContext) などは尊重します。）

### やってはいけないパターン

```go
// BAD: ctx を無視して context.Background() を新規に作る
func (h *TodoHandler) GetTodo(w http.ResponseWriter, r *http.Request) {
    t, err := h.store.Get(context.Background(), id) // ← 切断されてもキャンセルされない
    // ...
}
```

これをやると、クライアントが Ctrl-C しても DB クエリが最後まで走り、リソースを消費し続けます。
**`r.Context()` を素直に流す** のが基本形。

### 良いパターン

```go
func (h *TodoHandler) GetTodo(w http.ResponseWriter, r *http.Request) {
    t, err := h.store.Get(r.Context(), id) // ← 切断されたら Get 側でキャンセル可能
    // ...
}
```

そのために本教材の `Store` インターフェイスは第 1 引数に `ctx` を取ります。

```go
type Store interface {
    Create(ctx context.Context, title string) (Todo, error)
    Get(ctx context.Context, id int64) (Todo, error)
    // ...
}
```

「context を取らない Store」は将来の DB 差し替えで詰みます。**最初から入れておく**のが正解です。

## 5.3 [`context.WithValue`](https://pkg.go.dev/context#WithValue) の作法

context には値を載せられます。ミドルウェアで生成した **request ID** をハンドラに運ぶ用途などで使います。

```go
ctx := context.WithValue(r.Context(), someKey, "abcd1234")
r = r.WithContext(ctx)
```

ここで **キー** の作り方に流儀があります。

### 悪い例: `string` をキーにする

```go
ctx := context.WithValue(r.Context(), "request_id", id) // ← 危険
```

- 他のパッケージが同じ文字列キーを使うと **衝突** する
- Linter に警告される（`SA1029`）

### 良い例: 非公開の独自型をキーにする

```go
type ctxKey int

const (
    ctxKeyRequestID ctxKey = iota
)

func withRequestID(ctx context.Context, id string) context.Context {
    return context.WithValue(ctx, ctxKeyRequestID, id)
}

func RequestIDFromContext(ctx context.Context) string {
    v, _ := ctx.Value(ctxKeyRequestID).(string)
    return v
}
```

- `ctxKey` はパッケージ外に公開されないので **衝突が構造的に不可能**
- 「載せる関数」「取り出す関数」を対で用意すると、呼び出し側は型を意識しなくて済む

これが「context の値操作は必ずヘルパー関数越しに」というプラクティスの実体です。
`httpx.go` にこの 2 関数を書いておきます。

```go
// httpx.go
package main

import "context"

type ctxKey int

const (
    ctxKeyRequestID ctxKey = iota
)

func withRequestID(ctx context.Context, id string) context.Context {
    return context.WithValue(ctx, ctxKeyRequestID, id)
}

func RequestIDFromContext(ctx context.Context) string {
    v, _ := ctx.Value(ctxKeyRequestID).(string)
    return v
}
```

## 5.4 context に載せて良いもの・ダメなもの

**載せて良い**:

- request ID / trace ID
- 認証済みユーザーの ID や権限（ミドルウェアが検証済みの結果）
- リクエストごとに切り替えたい logger（`slog.Logger` に attrs を足したもの）

**載せてはいけない**:

- 関数の**引数として渡せば済むもの**（`storeID`・`limit` などのビジネス値）
- 大きな struct（本体データ）
- optional な設定値の代替

「context は関数シグネチャの補助であって、隠し引数バケツではない」と覚えておいてください。

## 5.5 覚えておくべき慣習

- **`ctx` は関数の第 1 引数**（第 2 以降にしない）
- **`ctx` を struct のフィールドに持たない**（1 つの struct が複数リクエストで共有される場合、context が混線する）
- **[`context.TODO()`](https://pkg.go.dev/context#TODO) は「今は決めきれないが後で埋める」の印**。プロダクションコードには残さない

## 次の章

[06. ドメインとストレージ層 →](./06-store.md)
