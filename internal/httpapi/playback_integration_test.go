//go:build integration

package httpapi_test

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nk-test-assignment-ufanet-iptv/internal/httpapi"
	"nk-test-assignment-ufanet-iptv/internal/iptv"
	"nk-test-assignment-ufanet-iptv/internal/postgres"
	"nk-test-assignment-ufanet-iptv/internal/testdb"
)

func TestPlaybackPostgreSQL(t *testing.T) {
	databaseURL, _ := testdb.IsolatedDatabase(t)
	pool, err := postgres.Open(t.Context(), databaseURL, 2)
	if err != nil {
		t.Fatal("open HTTP test pool failed")
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(t.Context(), pool); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Seed(t.Context(), pool); err != nil {
		t.Fatal(err)
	}
	repository := postgres.NewRepository(pool)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	server := httptest.NewServer(httpapi.NewRouter(repository, iptv.NewCatalog(repository), iptv.NewPlayer(repository), logger))
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 5 * time.Second

	for _, tc := range []struct {
		name       string
		id         string
		authorized bool
		wantStatus int
		wantBody   string
	}{
		{"missing auth", "1", false, http.StatusUnauthorized, `{"error":{"code":"unauthorized","message":"authentication required"}}`},
		{"active channel", "1", true, http.StatusOK, `{"channel_id":1,"url":"https://stream.example.com/live/ch1/index.m3u8"}`},
		{"disabled channel", "5", true, http.StatusNotFound, `{"error":{"code":"channel_not_found","message":"channel not found"}}`},
		{"missing channel", "9223372036854775807", true, http.StatusNotFound, `{"error":{"code":"channel_not_found","message":"channel not found"}}`},
		{"database failure", "1", true, http.StatusInternalServerError, `{"error":{"code":"internal_error","message":"internal server error"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.wantStatus == http.StatusInternalServerError {
				if _, err := pool.Exec(t.Context(), "DROP TABLE channels"); err != nil {
					t.Fatal(err)
				}
			}
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/api/v1/channels/"+tc.id+"/playback", nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.authorized {
				req.SetBasicAuth("viewer", "viewer_demo")
			}
			response, err := client.Do(req)
			if err != nil {
				t.Fatal("HTTP playback request failed")
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != tc.wantStatus || strings.TrimSpace(string(body)) != tc.wantBody {
				t.Fatalf("status = %d, body = %s; want status %d and %s", response.StatusCode, body, tc.wantStatus, tc.wantBody)
			}
			if response.Header.Get("Content-Type") != "application/json" {
				t.Fatal("playback response must be JSON")
			}
			if !tc.authorized && response.Header.Get("WWW-Authenticate") != `Basic realm="iptv"` {
				t.Fatal("unauthenticated playback request must include Basic Auth challenge")
			}
			if pool.Stat().AcquiredConns() != 0 {
				t.Fatal("HTTP playback request did not release its connections")
			}
		})
	}
	if !strings.Contains(logs.String(), "get playback failed") {
		t.Fatal("database failure must be logged")
	}
	for _, private := range []string{"SELECT", "SQLSTATE", "42P01", "viewer_demo", "postgres://"} {
		if strings.Contains(logs.String(), private) {
			t.Fatal("database details or credentials leaked into playback logs")
		}
	}
}
