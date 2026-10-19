package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"nk-test-assignment-ufanet-iptv/internal/config"
	"nk-test-assignment-ufanet-iptv/internal/httpapi"
	"nk-test-assignment-ufanet-iptv/internal/iptv"
	"nk-test-assignment-ufanet-iptv/internal/postgres"
)

func main() {
	os.Exit(run())
}

func run() int {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		return 1
	}
	logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))

	stop, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	pool, err := postgres.Open(stop, cfg.DatabaseURL, cfg.DBMaxConns)
	if err != nil {
		// Driver errors can include the DSN, so keep startup logs free of connection details.
		logger.Error("connect PostgreSQL failed")
		return 1
	}
	defer pool.Close()

	if err := postgres.Migrate(stop, pool); err != nil {
		logger.Error("migrate PostgreSQL failed")
		return 1
	}

	if err := postgres.Seed(stop, pool); err != nil {
		logger.Error("seed PostgreSQL failed")
		return 1
	}

	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		logger.Error("listen HTTP failed", "error", err)
		return 1
	}

	repository := postgres.NewRepository(pool)
	catalog := iptv.NewCatalog(repository)
	server := newHTTPServer(cfg.HTTPAddr, httpapi.NewRouter(repository, catalog, logger), logger)
	if err := serve(stop, server, listener, logger, shutdownTimeout); err != nil {
		logger.Error("HTTP server failed", "error", err)
		return 1
	}
	return 0
}
