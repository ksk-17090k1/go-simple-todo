package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
)

// Handler は各 HTTP ハンドラをメソッドとして持つ構造体。
// 必要な依存（Store, Logger）を struct のフィールドで受け取る＝依存性注入。
// これにより、テスト時は fake の Store と io.Discard に流す logger を渡せる。
type Handler struct {
	store  Store
	logger *slog.Logger
}

func NewHandler(store Store, logger *slog.Logger) *Handler {
	return &Handler{store: store, logger: logger}
}

// リクエストボディの最大サイズ（1 MiB）。
// これを超えるとハンドラで読み込み時にエラーになる。
const maxBodyBytes = 1 << 20

// --- Create ---

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

// --- List ---

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

// --- Get ---

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

// --- Update ---

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

// --- Delete ---

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

// --- helpers ---

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

// decodeJSON はリクエストボディを型 T にデコードする共通処理。
// エラー時はレスポンスを書き終えて ok=false を返す。呼び出し側は return するだけでよい。
func decodeJSON[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var v T
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return v, false
	}
	// 1 リクエスト = 1 JSON オブジェクトを強制する（末尾ゴミを弾く）。
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
