//go:build integration

package httpapi_test

import (
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

func TestChannelsPostgreSQL(t *testing.T) {
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
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	server := httptest.NewServer(httpapi.NewRouter(repository, iptv.NewCatalog(repository), logger))
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 5 * time.Second

	for _, tc := range []struct {
		name       string
		authorized bool
		wantStatus int
		wantBody   string
	}{
		{"missing auth", false, http.StatusUnauthorized, `{"error":{"code":"unauthorized","message":"authentication required"}}`},
		{"active viewer", true, http.StatusOK, `{"channels":[{"id":1,"number":1,"name":"Первый"},{"id":2,"number":2,"name":"Россия 1"},{"id":3,"number":3,"name":"Матч ТВ"},{"id":4,"number":4,"name":"НТВ"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/api/v1/channels", nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.authorized {
				req.SetBasicAuth("viewer", "viewer_demo")
			}
			response, err := client.Do(req)
			if err != nil {
				t.Fatal("HTTP catalog request failed")
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
				t.Fatal("catalog response must be JSON")
			}
			if !tc.authorized && response.Header.Get("WWW-Authenticate") != `Basic realm="iptv"` {
				t.Fatal("unauthenticated catalog request must include Basic Auth challenge")
			}
			if pool.Stat().AcquiredConns() != 0 {
				t.Fatal("HTTP catalog request did not release its connections")
			}
		})
	}
}
