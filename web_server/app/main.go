package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// run は main の実処理。エラーを戻り値で返すことでテストしやすくなる
// （ここではテストしていないが、慣習として推奨されるパターン）。
func run() error {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	// 1) ログ: 構造化 JSON でプロセス標準出力に流す。
	//    デフォルトロガーも同じものに差し替えておく。
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// 2) 依存を組み立てる。
	store := NewMemStore()
	handler := NewHandler(store, logger)

	// 3) ルーティング。
	mux := http.NewServeMux()
	registerRoutes(mux, handler)

	// 4) ミドルウェア。並びは「外→内」。
	//    requestID → logging → recovery → mux
	//    こうすると：
	//      - どのログにも request_id が載る
	//      - handler で panic しても、recovery が 500 を書いた後に
	//        logging のアクセスログが status=500 で正しく残る
	h := chain(mux,
		requestIDMiddleware,
		loggingMiddleware(logger),
		recoveryMiddleware(logger),
	)

	// 5) http.Server の設定。
	//    タイムアウトを明示的に入れることで slow-loris 系の攻撃や
	//    ぶら下がった接続によるリソース枯渇を防ぐ。
	srv := &http.Server{
		Addr:              *addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// 6) SIGINT / SIGTERM を受けたら ctx がキャンセルされる。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 7) サーバは別 goroutine で走らせる。main は ctx.Done() を待つ。
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("server starting", "addr", *addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	// 8) シャットダウン待機。
	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	// 9) graceful shutdown: 進行中のリクエストを 10 秒だけ待つ。
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	logger.Info("server stopped cleanly")
	return nil
}
