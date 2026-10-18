package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"nk-test-assignment-ufanet-iptv/internal/config"
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

	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		logger.Error("listen HTTP failed", "error", err)
		return 1
	}

	server := newHTTPServer(cfg.HTTPAddr, http.NewServeMux(), logger)
	if err := serve(stop, server, listener, logger, shutdownTimeout); err != nil {
		logger.Error("HTTP server failed", "error", err)
		return 1
	}
	return 0
}
