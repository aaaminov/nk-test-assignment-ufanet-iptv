package httpapi_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nk-test-assignment-ufanet-iptv/internal/auth"
	"nk-test-assignment-ufanet-iptv/internal/httpapi"
	"nk-test-assignment-ufanet-iptv/internal/iptv"
)

type userLookupFunc func(context.Context, string) (iptv.User, error)

func (f userLookupFunc) LookupUser(ctx context.Context, login string) (iptv.User, error) {
	return f(ctx, login)
}

func TestBasicAuth(t *testing.T) {
	const password = "test-password"
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	active := iptv.User{ID: 1, PasswordHash: hash, Active: true}
	inactive := active
	inactive.Active = false
	damaged := active
	damaged.PasswordHash = "damaged-test-hash"
	header := func(password string) string {
		return "Basic " + base64.StdEncoding.EncodeToString([]byte("test-login:"+password))
	}

	for _, tc := range []struct {
		name       string
		header     string
		user       iptv.User
		lookupErr  error
		wantStatus int
		wantLookup bool
	}{
		{"active user", header(password), active, nil, http.StatusNoContent, true},
		{"missing authorization", "", active, nil, http.StatusUnauthorized, false},
		{"malformed authorization", "Basic !!!", active, nil, http.StatusUnauthorized, false},
		{"wrong scheme", "Bearer test-token", active, nil, http.StatusUnauthorized, false},
		{"unknown user", header(password), iptv.User{}, fmt.Errorf("lookup: %w", iptv.ErrUserNotFound), http.StatusUnauthorized, true},
		{"inactive user", header(password), inactive, nil, http.StatusUnauthorized, true},
		{"wrong password", header("wrong-test-password"), active, nil, http.StatusUnauthorized, true},
		{"empty password", header(""), active, nil, http.StatusUnauthorized, true},
		{"overlong password", header(strings.Repeat("a", 73)), active, nil, http.StatusUnauthorized, true},
		{"damaged hash", header(password), damaged, nil, http.StatusUnauthorized, true},
		{"lookup failure", header(password), active, errors.New("sensitive-database-detail: SELECT password_hash"), http.StatusInternalServerError, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			lookedUp := false
			users := userLookupFunc(func(ctx context.Context, login string) (iptv.User, error) {
				lookedUp = true
				if login != "test-login" {
					t.Fatal("lookup received an incorrect login")
				}
				return tc.user, tc.lookupErr
			})
			reached := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reached = true
				w.WriteHeader(http.StatusNoContent)
			})
			r := httptest.NewRequest(http.MethodGet, "/protected", nil)
			r.Header.Set("Authorization", tc.header)
			w := httptest.NewRecorder()
			httpapi.BasicAuth(users, logger, next).ServeHTTP(w, r)

			if w.Code != tc.wantStatus || reached != (tc.wantStatus == http.StatusNoContent) {
				t.Fatalf("status = %d, next reached = %v; want status %d", w.Code, reached, tc.wantStatus)
			}
			if lookedUp != tc.wantLookup {
				t.Fatalf("lookup called = %v, want %v", lookedUp, tc.wantLookup)
			}
			challenge := w.Header().Get("WWW-Authenticate")
			if tc.wantStatus == http.StatusUnauthorized {
				if challenge != `Basic realm="iptv"` {
					t.Fatal("401 must include the Basic Auth challenge")
				}
			} else if challenge != "" {
				t.Fatal("non-401 response must not challenge for credentials")
			}

			switch tc.wantStatus {
			case http.StatusUnauthorized, http.StatusInternalServerError:
				wantBody := "{\"error\":{\"code\":\"unauthorized\",\"message\":\"authentication required\"}}\n"
				if tc.wantStatus == http.StatusInternalServerError {
					wantBody = "{\"error\":{\"code\":\"internal_error\",\"message\":\"internal server error\"}}\n"
				}
				if w.Header().Get("Content-Type") != "application/json" || w.Body.String() != wantBody {
					t.Fatal("error response must use the uniform JSON format and safe message")
				}
			case http.StatusNoContent:
				if w.Body.Len() != 0 {
					t.Fatal("middleware unexpectedly wrote a successful response body")
				}
			}
			if tc.wantStatus == http.StatusInternalServerError && !strings.Contains(logs.String(), "lookup user failed") {
				t.Fatal("lookup failure must be logged")
			}
			output := w.Body.String() + logs.String()
			for _, sensitive := range []string{password, hash, tc.user.PasswordHash, tc.header, "sensitive-database-detail", "SELECT password_hash"} {
				if sensitive != "" && strings.Contains(output, sensitive) {
					t.Fatal("credentials or database details leaked into response or logs")
				}
			}
		})
	}
}

func TestBasicAuthRequestContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	users := userLookupFunc(func(got context.Context, login string) (iptv.User, error) {
		if got != ctx {
			t.Fatal("lookup must receive the request context")
		}
		cancel()
		return iptv.User{}, got.Err()
	})
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("canceled lookup must not reach next")
	})
	r := httptest.NewRequest(http.MethodGet, "/protected", nil).WithContext(ctx)
	r.SetBasicAuth("test-login", "test-password")
	w := httptest.NewRecorder()
	httpapi.BasicAuth(users, logger, next).ServeHTTP(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("canceled lookup status = %d, want 500", w.Code)
	}
}
