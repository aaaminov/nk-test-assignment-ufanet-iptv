package httpapi_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nk-test-assignment-ufanet-iptv/internal/auth"
	"nk-test-assignment-ufanet-iptv/internal/httpapi"
	"nk-test-assignment-ufanet-iptv/internal/iptv"
)

type channelListerFunc func(context.Context) ([]iptv.Channel, error)

func (f channelListerFunc) ListChannels(ctx context.Context) ([]iptv.Channel, error) {
	return f(ctx)
}

func TestChannels(t *testing.T) {
	hash, err := auth.HashPassword("catalog-test-password")
	if err != nil {
		t.Fatal(err)
	}
	users := userLookupFunc(func(context.Context, string) (iptv.User, error) {
		return iptv.User{ID: 1, PasswordHash: hash, Active: true}, nil
	})
	for _, tc := range []struct {
		name       string
		channels   []iptv.Channel
		err        error
		wantStatus int
		wantBody   string
	}{
		{
			name: "catalog",
			channels: []iptv.Channel{
				{ID: 3, Number: 1, Name: "Первый"},
				{ID: 1, Number: 2, Name: "Россия 1"},
			},
			wantStatus: http.StatusOK,
			wantBody:   `{"channels":[{"id":3,"number":1,"name":"Первый"},{"id":1,"number":2,"name":"Россия 1"}]}`,
		},
		{"empty", nil, nil, http.StatusOK, `{"channels":[]}`},
		{
			name:       "catalog failure",
			channels:   []iptv.Channel{{ID: 1, Number: 1, Name: "partial-result"}},
			err:        errors.New("sensitive-database-detail: SELECT stream_url FROM channels"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"error":{"code":"internal_error","message":"internal server error"}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			called := false
			catalog := channelListerFunc(func(got context.Context) ([]iptv.Channel, error) {
				called = true
				if got != ctx {
					t.Fatal("catalog must receive the HTTP request context")
				}
				return tc.channels, tc.err
			})
			r := httptest.NewRequest(http.MethodGet, "/api/v1/channels", nil).WithContext(ctx)
			r.SetBasicAuth("catalog-test-user", "catalog-test-password")
			w := httptest.NewRecorder()
			httpapi.NewRouter(users, catalog, logger).ServeHTTP(w, r)

			if !called || w.Code != tc.wantStatus || w.Body.String() != tc.wantBody+"\n" {
				t.Fatalf("called = %v, status = %d, body = %s", called, w.Code, w.Body.String())
			}
			if w.Header().Get("Content-Type") != "application/json" {
				t.Fatal("catalog response must be JSON")
			}
			if tc.err != nil {
				if !strings.Contains(logs.String(), "list channels failed") {
					t.Fatal("catalog failure must be logged")
				}
				output := logs.String() + w.Body.String()
				for _, private := range []string{"sensitive-database-detail", "SELECT stream_url", "partial-result"} {
					if strings.Contains(output, private) {
						t.Fatal("database details or partial result leaked")
					}
				}
			}
		})
	}
}
