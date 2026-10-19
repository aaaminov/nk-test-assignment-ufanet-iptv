//go:build integration

package httpapi_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"nk-test-assignment-ufanet-iptv/internal/httpapi"
	"nk-test-assignment-ufanet-iptv/internal/iptv"
	"nk-test-assignment-ufanet-iptv/internal/postgres"
	"nk-test-assignment-ufanet-iptv/internal/testdb"
)

func TestChannelsPostgreSQLCancellation(t *testing.T) {
	for _, cancelClient := range []bool{true, false} {
		name := "SQL timeout"
		if cancelClient {
			name = "client cancellation"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			databaseURL, _ := testdb.IsolatedDatabase(t)
			// One slot makes a leaked connection observable on the next request.
			pool, err := postgres.Open(ctx, databaseURL, 1)
			if err != nil {
				t.Fatal("open HTTP test pool failed")
			}
			t.Cleanup(pool.Close)
			if err := postgres.Migrate(ctx, pool); err != nil {
				t.Fatal(err)
			}
			if err := postgres.Seed(ctx, pool); err != nil {
				t.Fatal(err)
			}
			blocker, err := pgx.Connect(ctx, databaseURL)
			if err != nil {
				t.Fatal("connect lock holder failed")
			}
			defer func() {
				cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := blocker.Close(cleanup); err != nil {
					t.Error("close lock holder failed")
				}
			}()
			if _, err := blocker.Exec(ctx, "BEGIN; LOCK TABLE channels IN ACCESS EXCLUSIVE MODE"); err != nil {
				t.Fatal("lock channels failed")
			}

			repository := postgres.NewRepository(pool)
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			router := httpapi.NewRouter(repository, iptv.NewCatalog(repository), iptv.NewPlayer(repository), logger)
			finished := make(chan struct{}, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer func() { finished <- struct{}{} }()
				router.ServeHTTP(w, r)
			}))
			t.Cleanup(server.Close)
			client := server.Client()
			client.Timeout = 5 * time.Second
			requestCtx, cancelRequest := context.WithCancel(ctx)
			defer cancelRequest()
			req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, server.URL+"/api/v1/channels", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.SetBasicAuth("viewer", "viewer_demo")
			result := make(chan error, 1)
			go func() {
				response, err := client.Do(req)
				if err == nil {
					defer response.Body.Close()
					body, readErr := io.ReadAll(response.Body)
					if readErr != nil {
						err = readErr
					} else if response.StatusCode != http.StatusInternalServerError ||
						response.Header.Get("Content-Type") != "application/json" ||
						strings.TrimSpace(string(body)) != `{"error":{"code":"internal_error","message":"internal server error"}}` {
						err = errors.New("SQL timeout must produce a safe JSON 500")
					}
				}
				result <- err
			}()

			// Observe the SQL wait before canceling; Auth must already have finished.
			waiting, cancelWaiting := context.WithTimeout(ctx, 2*time.Second)
			defer cancelWaiting()
			for {
				var blocked bool
				err := blocker.QueryRow(waiting, `SELECT EXISTS (
					SELECT FROM pg_locks WHERE relation = 'channels'::regclass AND NOT granted
				)`).Scan(&blocked)
				if err != nil {
					t.Fatal("observe blocked catalog query failed")
				}
				if blocked {
					break
				}
				select {
				case <-time.After(10 * time.Millisecond):
				case <-waiting.Done():
					t.Fatal("catalog query did not wait on the database lock")
				}
			}
			if cancelClient {
				cancelRequest()
			}
			select {
			case err := <-result:
				if cancelClient && !errors.Is(err, context.Canceled) || !cancelClient && err != nil {
					t.Fatal("unexpected result of blocked HTTP request")
				}
			case <-ctx.Done():
				t.Fatal("blocked HTTP request did not finish")
			}
			released, cancelReleased := context.WithTimeout(ctx, time.Second)
			defer cancelReleased()
			select {
			case <-finished:
			case <-released.Done():
				t.Fatal("handler did not finish after cancellation or SQL timeout")
			}
			// pgx may destroy a canceled connection asynchronously. With one slot,
			// Ping proves that capacity returns while channels is still locked.
			if err := pool.Ping(released); err != nil {
				t.Fatal("HTTP request did not release its database connection")
			}
			if !strings.Contains(logs.String(), "list channels failed") {
				t.Fatal("failed catalog request must be logged")
			}
			for _, private := range []string{"SELECT", "SQLSTATE", "viewer_demo", "Authorization", "postgres://", "password_hash"} {
				if strings.Contains(logs.String(), private) {
					t.Fatal("database details or credentials leaked into logs")
				}
			}
			if _, err := blocker.Exec(ctx, "ROLLBACK"); err != nil {
				t.Fatal("release channels lock failed")
			}
			response, err := client.Do(req.Clone(ctx))
			if err != nil {
				t.Fatal("pool capacity was not restored after failed HTTP request")
			}
			defer response.Body.Close()
			if _, err := io.Copy(io.Discard, response.Body); err != nil {
				t.Fatal("read recovered catalog failed")
			}
			if response.StatusCode != http.StatusOK {
				t.Fatal("catalog did not recover after failed HTTP request")
			}
		})
	}
}
