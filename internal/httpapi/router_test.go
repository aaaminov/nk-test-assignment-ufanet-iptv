package httpapi_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nk-test-assignment-ufanet-iptv/internal/auth"
	"nk-test-assignment-ufanet-iptv/internal/httpapi"
	"nk-test-assignment-ufanet-iptv/internal/iptv"
)

func TestRouterFallback(t *testing.T) {
	hash, err := auth.HashPassword("router-test-password")
	if err != nil {
		t.Fatal(err)
	}
	users := userLookupFunc(func(context.Context, string) (iptv.User, error) {
		return iptv.User{ID: 1, PasswordHash: hash, Active: true}, nil
	})
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	router := httpapi.NewRouter(users, nil, nil, logger)
	for _, tc := range []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{"unknown API path", http.MethodGet, "/api/v1/unknown", http.StatusNotFound},
		{"unknown path with POST", http.MethodPost, "/api/v1/unknown", http.StatusNotFound},
		{"API root", http.MethodGet, "/api/v1", http.StatusNotFound},
		{"API subtree root", http.MethodGet, "/api/v1/", http.StatusNotFound},
		{"trailing slash", http.MethodGet, "/api/v1/channels/", http.StatusNotFound},
		{"extra path segment", http.MethodGet, "/api/v1/channels/1/playback/extra", http.StatusNotFound},
		{"outside API", http.MethodGet, "/unknown", http.StatusNotFound},
		{"catalog method", http.MethodPost, "/api/v1/channels", http.StatusMethodNotAllowed},
		{"playback method", http.MethodDelete, "/api/v1/channels/1/playback", http.StatusMethodNotAllowed},
		{"method before invalid ID", http.MethodPost, "/api/v1/channels/invalid/playback", http.StatusMethodNotAllowed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, nil)
			if strings.HasPrefix(tc.path, "/api/v1") {
				r.SetBasicAuth("router-test-user", "router-test-password")
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)

			wantBody := `{"error":{"code":"not_found","message":"resource not found"}}` + "\n"
			wantAllow := ""
			if tc.wantStatus == http.StatusMethodNotAllowed {
				wantBody = `{"error":{"code":"method_not_allowed","message":"method not allowed"}}` + "\n"
				wantAllow = "GET, HEAD"
			}
			if w.Code != tc.wantStatus || w.Body.String() != wantBody {
				t.Fatalf("status = %d, body = %s; want status %d and %s", w.Code, w.Body.String(), tc.wantStatus, wantBody)
			}
			if w.Header().Get("Content-Type") != "application/json" || w.Header().Get("Allow") != wantAllow {
				t.Fatalf("unexpected fallback headers: %v", w.Header())
			}
		})
	}
}

func TestRouterAuthPrecedence(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	for _, tc := range []struct {
		name       string
		method     string
		path       string
		lookupFail bool
	}{
		{"auth before ID", http.MethodGet, "/api/v1/channels/invalid/playback", false},
		{"auth before catalog method", http.MethodPost, "/api/v1/channels", false},
		{"auth before playback method and ID", http.MethodPost, "/api/v1/channels/invalid/playback", false},
		{"auth before unknown path", http.MethodGet, "/api/v1/unknown", false},
		{"auth before repeated slashes", http.MethodGet, "/api/v1//unknown", false},
		{"auth before dot segment", http.MethodGet, "/api/v1/./unknown", false},
		{"auth before parent segment", http.MethodGet, "/api/v1/channels/../unknown", false},
		{"auth at API root", http.MethodGet, "/api/v1", false},
		{"lookup failure before method and ID", http.MethodPost, "/api/v1/channels/invalid/playback", true},
		{"lookup failure before redirect", http.MethodGet, "/api/v1//unknown", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			users := userLookupFunc(func(context.Context, string) (iptv.User, error) {
				if !tc.lookupFail {
					t.Fatal("missing credentials must not reach lookup")
				}
				return iptv.User{}, errors.New("private-database-detail")
			})
			r := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.lookupFail {
				r.SetBasicAuth("router-test-user", "router-test-password")
			}
			w := httptest.NewRecorder()
			httpapi.NewRouter(users, nil, nil, logger).ServeHTTP(w, r)

			wantStatus := http.StatusUnauthorized
			wantBody := `{"error":{"code":"unauthorized","message":"authentication required"}}` + "\n"
			wantChallenge := `Basic realm="iptv"`
			if tc.lookupFail {
				wantStatus = http.StatusInternalServerError
				wantBody = `{"error":{"code":"internal_error","message":"internal server error"}}` + "\n"
				wantChallenge = ""
			}
			if w.Code != wantStatus || w.Body.String() != wantBody {
				t.Fatalf("status = %d, body = %s; want status %d and %s", w.Code, w.Body.String(), wantStatus, wantBody)
			}
			if w.Header().Get("Content-Type") != "application/json" || w.Header().Get("WWW-Authenticate") != wantChallenge || w.Header().Get("Allow") != "" || w.Header().Get("Location") != "" {
				t.Fatalf("unexpected auth headers: %v", w.Header())
			}
		})
	}
}

func TestRouterAuthenticatedRedirect(t *testing.T) {
	hash, err := auth.HashPassword("redirect-test-password")
	if err != nil {
		t.Fatal(err)
	}
	lookedUp := false
	users := userLookupFunc(func(context.Context, string) (iptv.User, error) {
		lookedUp = true
		return iptv.User{ID: 1, PasswordHash: hash, Active: true}, nil
	})
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	r := httptest.NewRequest(http.MethodGet, "/api/v1//unknown?test=1", nil)
	r.SetBasicAuth("redirect-test-user", "redirect-test-password")
	w := httptest.NewRecorder()
	httpapi.NewRouter(users, nil, nil, logger).ServeHTTP(w, r)
	if !lookedUp {
		t.Fatal("authentication must run before redirect")
	}
	if w.Code != http.StatusTemporaryRedirect || w.Header().Get("Location") != "/api/v1/unknown?test=1" {
		t.Fatalf("status = %d, headers = %v; want canonical redirect", w.Code, w.Header())
	}
}

func TestRouterHEAD(t *testing.T) {
	hash, err := auth.HashPassword("head-test-password")
	if err != nil {
		t.Fatal(err)
	}
	users := userLookupFunc(func(context.Context, string) (iptv.User, error) {
		return iptv.User{ID: 1, PasswordHash: hash, Active: true}, nil
	})
	catalog := channelListerFunc(func(context.Context) ([]iptv.Channel, error) {
		return []iptv.Channel{{ID: 1, Number: 1, Name: "Первый"}}, nil
	})
	player := playbackGetterFunc(func(ctx context.Context, id int64) (iptv.Playback, error) {
		switch id {
		case 1:
			return iptv.Playback{ChannelID: 1, URL: "https://stream.example.com/live/ch1/index.m3u8"}, nil
		case 2:
			return iptv.Playback{}, iptv.ErrChannelNotFound
		default:
			return iptv.Playback{}, errors.New("private-database-detail")
		}
	})
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	server := httptest.NewServer(httpapi.NewRouter(users, catalog, player, logger))
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 5 * time.Second
	for _, tc := range []struct {
		name       string
		path       string
		authorized bool
		wantStatus int
	}{
		{"catalog", "/api/v1/channels", true, http.StatusOK},
		{"playback", "/api/v1/channels/1/playback", true, http.StatusOK},
		{"invalid ID", "/api/v1/channels/invalid/playback", true, http.StatusBadRequest},
		{"unavailable channel", "/api/v1/channels/2/playback", true, http.StatusNotFound},
		{"internal error", "/api/v1/channels/3/playback", true, http.StatusInternalServerError},
		{"unknown path", "/api/v1/unknown", true, http.StatusNotFound},
		{"missing auth", "/api/v1/channels/invalid/playback", false, http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var getHeader http.Header
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				r, err := http.NewRequestWithContext(t.Context(), method, server.URL+tc.path, nil)
				if err != nil {
					t.Fatal(err)
				}
				if tc.authorized {
					r.SetBasicAuth("head-test-user", "head-test-password")
				}
				response, err := client.Do(r)
				if err != nil {
					t.Fatal(err)
				}
				body, readErr := io.ReadAll(response.Body)
				closeErr := response.Body.Close()
				if readErr != nil || closeErr != nil {
					t.Fatalf("read body: %v; close body: %v", readErr, closeErr)
				}
				if response.StatusCode != tc.wantStatus || response.Header.Get("Content-Type") != "application/json" {
					t.Fatalf("%s: status = %d, headers = %v", method, response.StatusCode, response.Header)
				}
				if method == http.MethodGet {
					if len(body) == 0 {
						t.Fatal("GET must return a JSON body")
					}
					getHeader = response.Header
					continue
				}
				if len(body) != 0 {
					t.Fatalf("HEAD returned a body: %q", body)
				}
				for _, header := range []string{"Content-Type", "Content-Length", "WWW-Authenticate"} {
					if response.Header.Get(header) != getHeader.Get(header) {
						t.Errorf("HEAD %s = %q; GET = %q", header, response.Header.Get(header), getHeader.Get(header))
					}
				}
			}
		})
	}
}
