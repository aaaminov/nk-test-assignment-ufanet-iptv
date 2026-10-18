package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

const shutdownTimeout = 10 * time.Second

func newHTTPServer(addr string, handler http.Handler, logger *slog.Logger) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
}

func serve(stop context.Context, server *http.Server, listener net.Listener, logger *slog.Logger, timeout time.Duration) error {
	// The signal context controls shutdown, not the lifetime of in-flight requests.
	requests, cancelRequests := context.WithCancel(context.Background())
	defer cancelRequests()
	server.BaseContext = func(net.Listener) context.Context { return requests }

	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	logger.Info("HTTP server started", "addr", listener.Addr().String())

	select {
	case err := <-served:
		cancelRequests()
		closeErr := server.Close()
		if !errors.Is(err, http.ErrServerClosed) {
			return errors.Join(fmt.Errorf("serve HTTP: %w", err), closeErr)
		}
		return closeErr
	case <-stop.Done():
		logger.Info("HTTP server stopping")
	}

	shutdown, cancelShutdown := context.WithTimeout(context.Background(), timeout)
	defer cancelShutdown()
	if err := server.Shutdown(shutdown); err != nil {
		cancelRequests()
		return errors.Join(fmt.Errorf("shutdown HTTP: %w", err), server.Close())
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP: %w", err)
	}
	logger.Info("HTTP server stopped")
	return nil
}
