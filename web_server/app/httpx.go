package main

import (
	"context"
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
		// この段階では既にステータスとヘッダは flush 済みなので
		// クライアントに返せる手段はない。ログだけ残す。
		slog.Error("failed to encode json response", "err", err)
	}
}

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

// --- context helpers ---
//
// context に載せる値のキーは、外部パッケージからのアクセス衝突を防ぐため
// 非公開の独自型で定義する（string を直接キーにしない）。

type ctxKey int

const (
	ctxKeyRequestID ctxKey = iota
)

func withRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyRequestID, id)
}

// RequestIDFromContext は context に載せた request ID を取り出す。
// 未セット時は空文字列を返す。
func RequestIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyRequestID).(string)
	return v
}
