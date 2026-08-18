# 07. ハンドラの実装

## この章のゴール

- `Handler` 構造体に依存を注入する形を身につける
- CRUD 5 本を書き切る
- ステータスコードとレスポンス形状の意図を意識する

## 7.1 なぜ `Handler` 構造体を作るのか

前章までのハンドラは関数リテラルで書いていました。次のように書きたくなります。

```go
mux.HandleFunc("POST /todos", func(w http.ResponseWriter, r *http.Request) {
    store.Create(...) // ← store はどこから？ グローバル変数？
})
```

依存（`store`, `logger`）をグローバル変数で持つと、テストや置き換えができません。
そこで **struct にまとめて注入する** パターンを採ります。

```go
// handler.go
package main

import "log/slog"

type Handler struct {
	store  Store
	logger *slog.Logger
}

func NewHandler(store Store, logger *slog.Logger) *Handler {
	return &Handler{store: store, logger: logger}
}
```

各エンドポイントは `*Handler` のメソッドにします。ルーティング側では **メソッド値** を渡します。

```go
mux.HandleFunc("POST /todos", h.CreateTodo)
```

`h.CreateTodo` はレシーバがバインドされた関数値。`http.HandlerFunc` として使えます。

## 7.2 共通ヘルパー

前章と 04 章で作ったヘルパーを `handler.go` にまとめて持ってきます。

```go
import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
)

const maxBodyBytes = 1 << 20

func parsePathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_path", "id must be a positive integer")
		return 0, false
	}
	return id, true
}

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

// serverError は 500 系エラーを共通のフォーマットで返す。ログには request ID を入れる。
func (h *Handler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	h.logger.LogAttrs(r.Context(), slog.LevelError, "handler error",
		slog.String("op", op),
		slog.String("err", err.Error()),
		slog.String("request_id", RequestIDFromContext(r.Context())),
	)
	writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
}
```

`serverError` は「エラー内容はログ、クライアントには汎化されたメッセージ」を実現するヘルパーです。
**内部エラーの詳細をクライアントに漏らさない** のは基本的なセキュリティプラクティスです。

## 7.3 Create

```go
type createTodoReq struct {
	Title string `json:"title"`
}

func (h *Handler) CreateTodo(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[createTodoReq](w, r)
	if !ok {
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "title is required")
		return
	}
	t, err := h.store.Create(r.Context(), title)
	if err != nil {
		h.serverError(w, r, "create todo", err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}
```

- **`201 Created`** を返す（`200` ではない）。作成 API の慣習。
- リクエスト用 struct は非公開の `createTodoReq`。「ドメイン型 `Todo` を直接デコードしない」のは、
  クライアントが `id` や `created_at` を送りつけて上書きしてくるのを防ぐため。

## 7.4 List

```go
func (h *Handler) ListTodos(w http.ResponseWriter, r *http.Request) {
	todos, err := h.store.List(r.Context())
	if err != nil {
		h.serverError(w, r, "list todos", err)
		return
	}
	// 空リストでも "todos": [] を返す（nil のままだと "todos": null になる）。
	if todos == nil {
		todos = []Todo{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"todos": todos})
}
```

### なぜ空スライスを `[]Todo{}` にするか

Go の `nil` スライスは JSON エンコード時に `null` になります。
クライアントは `data.todos.length` みたいに使う想定なので、`null` が来ると

```js
Cannot read properties of null (reading 'length')
```

で落ちます。**「空 = 空配列」を JSON で表現するため、明示的に `[]Todo{}` にする**。地味だが重要。

## 7.5 Get

```go
func (h *Handler) GetTodo(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePathID(w, r)
	if !ok {
		return
	}
	t, err := h.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrTodoNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "todo not found")
			return
		}
		h.serverError(w, r, "get todo", err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}
```

- `errors.Is` で **sentinel error** を判定 → 404 に変換。
- それ以外は 500。ログを残す。

## 7.6 Update

```go
type updateTodoReq struct {
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

func (h *Handler) UpdateTodo(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePathID(w, r)
	if !ok {
		return
	}
	req, ok := decodeJSON[updateTodoReq](w, r)
	if !ok {
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "title is required")
		return
	}
	t, err := h.store.Update(r.Context(), id, title, req.Done)
	if err != nil {
		if errors.Is(err, ErrTodoNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "todo not found")
			return
		}
		h.serverError(w, r, "update todo", err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}
```

- HTTP メソッドは `PUT`。**全項目を送りつけて全面置換** するセマンティクス。
- **`PATCH`（差分更新）** を採用する場合は「送られなかったフィールドは変更しない」というルールになり、
  Go 側では `*string`（`nil` = 未指定）を使うなど設計が一段複雑になります。今回は入門なので `PUT`。

## 7.7 Delete

```go
func (h *Handler) DeleteTodo(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePathID(w, r)
	if !ok {
		return
	}
	if err := h.store.Delete(r.Context(), id); err != nil {
		if errors.Is(err, ErrTodoNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "todo not found")
			return
		}
		h.serverError(w, r, "delete todo", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- **`204 No Content`** で **ボディを返さない**。削除の慣習。
- 「ボディ空」なので `writeJSON` は呼ばない。`w.WriteHeader(...)` だけ。

## 7.8 ルーティングをまとめる

`routes.go` を作って登録関数を切り出します。

```go
// routes.go
package main

import "net/http"

func registerRoutes(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("POST /todos", h.CreateTodo)
	mux.HandleFunc("GET /todos", h.ListTodos)
	mux.HandleFunc("GET /todos/{id}", h.GetTodo)
	mux.HandleFunc("PUT /todos/{id}", h.UpdateTodo)
	mux.HandleFunc("DELETE /todos/{id}", h.DeleteTodo)

	// ヘルスチェック用エンドポイント。監視・オーケストレータからの readiness 確認に。
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
}
```

`main.go` から `registerRoutes(mux, handler)` と呼ぶだけで済むようになります。

## 7.9 ステータスコードの早見表（今回使ったもの）

| 状況                         | ステータス          | ボディ                                        |
|------------------------------|---------------------|-----------------------------------------------|
| 作成成功                     | `201 Created`       | 作成された Todo                                |
| 取得・更新・一覧の成功       | `200 OK`            | 対象データ                                     |
| 削除成功                     | `204 No Content`    | なし                                          |
| リクエスト形式エラー         | `400 Bad Request`   | `{"error": {"code": ..., "message": ...}}`     |
| リソース不在                 | `404 Not Found`     | 同上                                          |
| メソッド不許可（自動応答）   | `405 Method Not Allowed` | Go 1.22+ の ServeMux が自動で返す           |
| 未捕捉のサーバ側エラー       | `500 Internal Server Error` | 汎化されたメッセージ（詳細はサーバログへ）    |

## 次の章

[08. ミドルウェア →](./08-middleware.md)
