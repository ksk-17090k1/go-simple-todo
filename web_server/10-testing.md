# 10. テスト

## この章のゴール

- `net/http/httptest` を使ってハンドラを単体テストする
- テーブル駆動テストで入力ケースを網羅する
- Store と Handler を **別々に** テストする理由を理解する

## 10.1 なぜテストが書きやすいのか

Go の `net/http` は、ハンドラを単体テストするための道具が **標準で** 揃っています。

- `httptest.NewRequest(method, target, body)` … リクエストを組み立てる
- `httptest.NewRecorder()` … `ResponseWriter` を実装した、記憶する箱

この 2 つがあれば、**サーバを立ち上げずに** ハンドラを呼べます。
「フレームワーク特有のテスト補助 API」に頼らず、標準ライブラリだけで完結する。ここが Go の強みです。

## 10.2 テスト用の mux 構築

`handler_test.go` を作ります。まず共通ヘルパー。

```go
// handler_test.go
package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func newTestMux(t *testing.T) (*http.ServeMux, Store) {
	t.Helper()
	store := NewMemStore()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil)) // テスト出力を汚さない
	h := NewHandler(store, logger)
	mux := http.NewServeMux()
	registerRoutes(mux, h)
	return mux, store
}

func do(mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}
```

### ポイント

- **`slog.NewJSONHandler(io.Discard, nil)`**: テスト中のログを捨てる。テスト実行時の出力を汚さない。
- **`newTestMux` は Store を返す**: テストが直接 Store にデータを積みたいときに使う（HTTP 経由で Create するより速い・失敗ポイントを減らせる）。
- **`do` ヘルパー**: リクエスト組み立てのボイラープレートを 1 箇所に集約。

## 10.3 テーブル駆動: Create の異常系

```go
func TestCreateTodo(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{"ok", `{"title":"buy milk"}`, http.StatusCreated},
		{"empty title", `{"title":"  "}`, http.StatusBadRequest},
		{"invalid json", `{`, http.StatusBadRequest},
		{"unknown field", `{"title":"x","foo":1}`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux, _ := newTestMux(t)
			w := do(mux, http.MethodPost, "/todos", tt.body)
			if w.Code != tt.wantStatus {
				t.Fatalf("status: want %d, got %d body=%s", tt.wantStatus, w.Code, w.Body.String())
			}
			if tt.wantStatus == http.StatusCreated {
				var got Todo
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if got.ID == 0 || got.Title != "buy milk" {
					t.Fatalf("unexpected body: %+v", got)
				}
				if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
					t.Fatalf("Content-Type: %s", ct)
				}
			}
		})
	}
}
```

### テーブル駆動のうまみ

- ケースを **1 個の struct スライス** で列挙 → 追加が「行を足す」だけになる
- `t.Run(name, func)` で **サブテスト化** → 失敗時にどのケースかがすぐ分かる
- 「異常系を体系的に潰す」網目が張れる

## 10.4 Get: 正常・404・パス不正

```go
func TestGetTodo(t *testing.T) {
	mux, store := newTestMux(t)
	created, _ := store.Create(context.Background(), "read a book")

	t.Run("found", func(t *testing.T) {
		w := do(mux, http.MethodGet, "/todos/"+strconv.FormatInt(created.ID, 10), "")
		if w.Code != http.StatusOK {
			t.Fatalf("status %d", w.Code)
		}
		var got Todo
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.Title != "read a book" {
			t.Fatalf("wrong title: %s", got.Title)
		}
	})

	t.Run("not found", func(t *testing.T) {
		w := do(mux, http.MethodGet, "/todos/99999", "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("status %d", w.Code)
		}
	})

	t.Run("bad path", func(t *testing.T) {
		w := do(mux, http.MethodGet, "/todos/not-an-int", "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status %d", w.Code)
		}
	})
}
```

「作る → 参照する」の順序を **HTTP 経由でなく Store 直叩き** で行っています。
テストしたい対象は GET なので、事前準備は最短経路で。

## 10.5 Update + Delete の一貫性テスト

「更新した内容が反映される」「削除後は 404 になる」という **契約** を検証します。

```go
func TestUpdateAndDeleteTodo(t *testing.T) {
	mux, store := newTestMux(t)
	created, _ := store.Create(context.Background(), "walk")

	w := do(mux, http.MethodPut, "/todos/"+strconv.FormatInt(created.ID, 10),
		`{"title":"walk the dog","done":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("update status %d body=%s", w.Code, w.Body.String())
	}
	var updated Todo
	_ = json.Unmarshal(w.Body.Bytes(), &updated)
	if !updated.Done || updated.Title != "walk the dog" {
		t.Fatalf("update not applied: %+v", updated)
	}

	w = do(mux, http.MethodDelete, "/todos/"+strconv.FormatInt(created.ID, 10), "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete status %d", w.Code)
	}

	w = do(mux, http.MethodGet, "/todos/"+strconv.FormatInt(created.ID, 10), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", w.Code)
	}
}
```

## 10.6 List: 空リストは `[]`（`null` ではない）

[章 07](./07-handlers.md) で「空スライスは `[]Todo{}` にする」と決めた契約を、テストで固定します。

```go
func TestListTodosEmptyReturnsArray(t *testing.T) {
	mux, _ := newTestMux(t)
	w := do(mux, http.MethodGet, "/todos", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"todos":[]`) {
		t.Fatalf("expected empty array, got %s", w.Body.String())
	}
}
```

## 10.7 Go 1.22+ の 405 自動応答をテストする

```go
func TestMethodNotAllowed(t *testing.T) {
	mux, _ := newTestMux(t)
	w := do(mux, http.MethodPatch, "/todos", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}
```

「拡張 ServeMux が 405 を返すこと」自体は Go 標準ライブラリの責任範囲ですが、
**自分たちのルーティング登録が正しい**（`GET /todos` と `POST /todos` の両方が生きている）ことのカナリアになるので、1 本入れておく価値があります。

## 10.8 Store 単体テスト

Handler 経由でなく Store を直接テストします。

```go
// store_test.go
package main

import (
	"context"
	"errors"
	"testing"
)

func TestMemStore_CRUD(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()

	// Create
	a, err := s.Create(ctx, "buy milk")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if a.ID == 0 || a.Title != "buy milk" || a.Done {
		t.Fatalf("unexpected created todo: %+v", a)
	}
	// ... 後略、完成コードを参照
}

func TestMemStore_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := NewMemStore()
	if _, err := s.Create(ctx, "x"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
```

### なぜ Store を単体で持つか

- **Handler と Store を切り分けてバグを切り分ける**: HTTP 経由のテストが落ちたとき、Store 側の問題か Handler 側の問題か、ログを見なくても分かる
- **速い**: HTTP レイヤーの往復コストがない
- **`ctx.Err()` 早期リターンの検証**が HTTP テストではやりにくい（クライアント側で切断を再現するのが手間）

## 10.9 実行

```sh
cd web_server/app
go test ./...
# ok  todoapi  0.234s
```

カバレッジも見たい場合:

```sh
go test -cover ./...
# ok  todoapi  0.234s  coverage: 78.4% of statements
```

## 10.10 これ以上のテスト（本編では扱わない）

- **`httptest.NewServer`** … 本物の TCP リッスンを立てて **クライアント側からも** 叩けるようにする。
  今回はミドルウェア込みで走らせても値打ちが薄いので割愛（ミドルウェアも `mux.ServeHTTP` で通る）。
  ネットワーク越しに叩きたい場合（TLS を含めるなど）に使うと便利。
- **並行テスト** … `t.Parallel()` で並列化。今回のように状態を持つ Store をテスト間で共有していない場合は素直に入れられる。
- **fuzzing** (`go test -fuzz=Fuzz`) … JSON パーサ周りの境界値を自動探索。入門としては別トピック。

## 次の章

[11. 付録 →](./11-appendix.md)
