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

// newTestMux は各テストで使う「TodoHandler をセットしただけの ServeMux」を返す。
// logger は捨てる（テスト出力を汚さないため）。
func newTestMux(t *testing.T) (*http.ServeMux, Store) {
	t.Helper()
	store := NewMemStore()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	h := NewTodoHandler(store, logger)
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

func TestUpdateAndDeleteTodo(t *testing.T) {
	mux, store := newTestMux(t)
	created, _ := store.Create(context.Background(), "walk")

	// update
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

	// delete
	w = do(mux, http.MethodDelete, "/todos/"+strconv.FormatInt(created.ID, 10), "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete status %d", w.Code)
	}

	// もう一度 get すると 404
	w = do(mux, http.MethodGet, "/todos/"+strconv.FormatInt(created.ID, 10), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", w.Code)
	}
}

func TestListTodosEmptyReturnsArray(t *testing.T) {
	mux, _ := newTestMux(t)
	w := do(mux, http.MethodGet, "/todos", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	// nil のままだと "todos": null になってしまう。テストで契約を固定しておく。
	if !strings.Contains(w.Body.String(), `"todos":[]`) {
		t.Fatalf("expected empty array, got %s", w.Body.String())
	}
}

func TestMethodNotAllowed(t *testing.T) {
	// Go 1.22+ の ServeMux は "POST /todos" 等の method 指定パターンにより
	// 不許可メソッドに対して自動で 405 を返す。
	mux, _ := newTestMux(t)
	w := do(mux, http.MethodPatch, "/todos", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}
