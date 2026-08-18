package main

import "net/http"

// registerRoutes は Handler のメソッドを ServeMux に紐づける。
// Go 1.22 以降の ServeMux は "METHOD /path" と {name} プレースホルダをサポートする。
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
