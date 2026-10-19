package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestShutdown(t *testing.T) {
	for _, tc := range []struct {
		name    string
		timeout time.Duration
		wantErr error
	}{
		{"graceful", 3 * time.Second, nil},
		{"deadline", 50 * time.Millisecond, context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entered := make(chan context.Context, 1)
			finish := make(chan struct{}, 1)
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				entered <- r.Context()
				<-finish
			})
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
			server := newHTTPServer(listener.Addr().String(), handler, logger)
			defer server.Close()

			stopping := make(chan struct{})
			server.RegisterOnShutdown(func() { close(stopping) })
			stop, cancel := context.WithCancel(context.Background())
			defer cancel()
			defer close(finish)
			stopped := make(chan error, 1)
			go func() { stopped <- serve(stop, server, listener, logger, tc.timeout) }()

			transport := &http.Transport{DisableKeepAlives: true}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
			response := make(chan error, 1)
			go func() {
				res, err := client.Get("http://" + listener.Addr().String())
				if err == nil {
					err = res.Body.Close()
				}
				response <- err
			}()

			var request context.Context
			select {
			case request = <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("request did not reach the handler")
			}
			cancel()
			select {
			case <-stopping:
			case <-time.After(5 * time.Second):
				t.Fatal("shutdown did not start")
			}

			// Shutdown callbacks can run before the listener has finished closing.
			until := time.Now().Add(time.Second)
			for {
				conn, err := net.DialTimeout("tcp", listener.Addr().String(), 100*time.Millisecond)
				if err != nil {
					break
				}
				conn.Close()
				if time.Now().After(until) {
					t.Fatal("server still accepts new connections during shutdown")
				}
				time.Sleep(time.Millisecond)
			}

			if tc.wantErr == nil {
				if request.Err() != nil {
					t.Fatal("request canceled before graceful shutdown finished")
				}
				finish <- struct{}{}
			}
			select {
			case err := <-stopped:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("shutdown error: got %v, want %v", err, tc.wantErr)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("shutdown did not finish")
			}

			if tc.wantErr != nil && request.Err() == nil {
				t.Fatal("request context was not canceled after shutdown deadline")
			}
			select {
			case err := <-response:
				if (err != nil) != (tc.wantErr != nil) {
					t.Fatalf("request error: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("client connection was not closed")
			}
		})
	}
}
