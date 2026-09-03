package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/config"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
)

type healthResponse struct {
	Status string `json:"status"`
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 15*time.Second)
	store, err := postgres.Open(startupCtx, cfg.DatabaseURL, postgres.Options{
		MaxOpenConns:    cfg.DBMaxOpenConns,
		MaxIdleConns:    cfg.DBMaxIdleConns,
		ConnMaxIdle:     cfg.DBConnMaxIdle,
		ConnMaxLifetime: cfg.DBConnMaxLifetime,
	})
	cancelStartup()
	if err != nil {
		slog.Error("connect postgres", "error", err)
		os.Exit(1)
	}
	defer func() { _ = store.Close() }()

	migrationCtx, cancelMigration := context.WithTimeout(context.Background(), 30*time.Second)
	err = store.ApplyMigrations(migrationCtx)
	cancelMigration()
	if err != nil {
		slog.Error("apply migrations", "error", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := store.Ping(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, healthResponse{Status: "not_ready"})
			return
		}
		writeJSON(w, http.StatusOK, healthResponse{Status: "ready"})
	})

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("synfactory api listening", "addr", cfg.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("api server failed", "error", err)
			stop()
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("api shutdown failed", "error", err)
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
