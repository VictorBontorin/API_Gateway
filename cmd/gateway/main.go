package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/VictorBontorin/api-gateway/internal/config"
	"github.com/VictorBontorin/api-gateway/internal/gateway"
	"github.com/VictorBontorin/api-gateway/internal/observability"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		observability.NewLogger("dev").Error("config", "err", err)
		os.Exit(1)
	}
	log := observability.NewLogger(cfg.Env)

	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer rdb.Close()

	h, err := gateway.New(cfg, rdb, log)
	if err != nil {
		log.Error("gateway", "err", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Info("gateway iniciado", "addr", cfg.Addr, "env", cfg.Env, "routes", len(cfg.Routes))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	log.Info("gateway encerrado")
}
