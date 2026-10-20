package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nk-test-assignment-ufanet-iptv/internal/auth"
	"nk-test-assignment-ufanet-iptv/internal/httpapi"
	"nk-test-assignment-ufanet-iptv/internal/iptv"
)

func TestRequestLogging(t *testing.T) {
	hash, err := auth.HashPassword("access-test-password")
	if err != nil {
		t.Fatal(err)
	}
	users := userLookupFunc(func(context.Context, string) (iptv.User, error) {
		return iptv.User{ID: 1, PasswordHash: hash, Active: true}, nil
	})
	for _, tc := range []struct {
		name       string
		method     string
		path       string
		authorized bool
		catalogErr error
		wantStatus int
	}{
		{"success", http.MethodGet, "/api/v1/channels", true, nil, http.StatusOK},
		{"HEAD", http.MethodHead, "/api/v1/channels", true, nil, http.StatusOK},
		{"unauthorized", http.MethodGet, "/api/v1/channels", false, nil, http.StatusUnauthorized},
		{"outside API", http.MethodGet, "/missing", false, nil, http.StatusNotFound},
		{"internal error", http.MethodGet, "/api/v1/channels", true, errors.New("private-test-db-detail"), http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			catalog := channelListerFunc(func(context.Context, int64) ([]iptv.Channel, error) {
				return nil, tc.catalogErr
			})
			r := httptest.NewRequest(tc.method, tc.path+"?token=private-test-query", strings.NewReader("private-test-body"))
			r.Header.Set("X-Test-Private", "private-test-header")
			r.AddCookie(&http.Cookie{Name: "session", Value: "private-test-cookie"})
			if tc.authorized {
				r.SetBasicAuth("access-test-user", "access-test-password")
			}
			w := httptest.NewRecorder()
			httpapi.NewRouter(users, catalog, nil, logger).ServeHTTP(w, r)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tc.wantStatus)
			}

			output := logs.String()
			decoder := json.NewDecoder(strings.NewReader(output))
			var access map[string]any
			count := 0
			for {
				var record map[string]any
				err := decoder.Decode(&record)
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatalf("decode log: %v", err)
				}
				if record["msg"] == "HTTP request" {
					access = record
					count++
				}
			}
			if count != 1 {
				t.Fatalf("access log count = %d, want 1", count)
			}
			if access["level"] != "INFO" || access["method"] != tc.method || access["path"] != tc.path || access["status"] != float64(tc.wantStatus) {
				t.Fatalf("unexpected access log: %v", access)
			}
			if duration, ok := access["duration_ms"].(float64); !ok || duration < 0 {
				t.Fatalf("duration_ms = %v, want non-negative milliseconds", access["duration_ms"])
			}
			for _, private := range []string{"access-test-user", "access-test-password", hash, r.Header.Get("Authorization"), "private-test-query", "private-test-body", "private-test-header", "private-test-cookie", "private-test-db-detail"} {
				if private != "" && strings.Contains(output, private) {
					t.Fatal("private request or database details leaked into logs")
				}
			}
		})
	}
}
