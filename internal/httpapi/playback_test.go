package httpapi_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nk-test-assignment-ufanet-iptv/internal/auth"
	"nk-test-assignment-ufanet-iptv/internal/httpapi"
	"nk-test-assignment-ufanet-iptv/internal/iptv"
)

type playbackGetterFunc func(context.Context, int64, int64) (iptv.Playback, error)

func (f playbackGetterFunc) GetPlayback(ctx context.Context, userID, id int64) (iptv.Playback, error) {
	return f(ctx, userID, id)
}

func TestPlayback(t *testing.T) {
	hash, err := auth.HashPassword("playback-test-password")
	if err != nil {
		t.Fatal(err)
	}
	users := userLookupFunc(func(context.Context, string) (iptv.User, error) {
		return iptv.User{ID: 7, PasswordHash: hash, Active: true}, nil
	})
	const invalidBody = `{"error":{"code":"invalid_id","message":"channel id must be a positive int64"}}`
	for _, tc := range []struct {
		name       string
		id         string
		wantID     int64
		err        error
		wantStatus int
		wantBody   string
	}{
		{"active channel", "42", 42, nil, http.StatusOK, `{"channel_id":42,"url":"https://stream.example.com/live/custom/index.m3u8"}`},
		{"largest valid ID", "9223372036854775807", math.MaxInt64, nil, http.StatusOK, `{"channel_id":9223372036854775807,"url":"https://stream.example.com/live/custom/index.m3u8"}`},
		{"invalid ID", "abc", 0, nil, http.StatusBadRequest, invalidBody},
		{"zero ID", "0", 0, nil, http.StatusBadRequest, invalidBody},
		{"negative ID", "-1", 0, nil, http.StatusBadRequest, invalidBody},
		{"overflow", "9223372036854775808", 0, nil, http.StatusBadRequest, invalidBody},
		{"fractional ID", "1.5", 0, nil, http.StatusBadRequest, invalidBody},
		{"SQL metacharacters", "1%20OR%201=1", 0, nil, http.StatusBadRequest, invalidBody},
		{"unavailable channel", "5", 5, fmt.Errorf("lookup: %w", iptv.ErrChannelNotFound), http.StatusNotFound, `{"error":{"code":"channel_not_found","message":"channel not found"}}`},
		{"subscription required", "42", 42, fmt.Errorf("lookup: %w", iptv.ErrSubscriptionRequired), http.StatusForbidden, `{"error":{"code":"subscription_required","message":"active subscription required"}}`},
		{"store failure", "42", 42, errors.New("sensitive-database-detail: SELECT stream_url FROM channels"), http.StatusInternalServerError, `{"error":{"code":"internal_error","message":"internal server error"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			type requestKey struct{}
			ctx, cancel := context.WithCancel(context.WithValue(t.Context(), requestKey{}, "playback-request"))
			defer cancel()
			called := false
			player := playbackGetterFunc(func(got context.Context, userID, id int64) (iptv.Playback, error) {
				called = true
				if got.Value(requestKey{}) != "playback-request" || got.Done() != ctx.Done() || userID != 7 || id != tc.wantID {
					t.Fatal("player must receive the request context, authenticated user ID and parsed channel ID")
				}
				if tc.err != nil {
					return iptv.Playback{ChannelID: id, URL: "partial-result"}, tc.err
				}
				return iptv.Playback{ChannelID: id, URL: "https://stream.example.com/live/custom/index.m3u8"}, nil
			})
			r := httptest.NewRequest(http.MethodGet, "/api/v1/channels/"+tc.id+"/playback?user_id=99", nil).WithContext(ctx)
			r.Header.Set("X-User-ID", "99")
			r.SetBasicAuth("playback-test-user", "playback-test-password")
			w := httptest.NewRecorder()
			httpapi.NewRouter(users, nil, player, logger).ServeHTTP(w, r)

			if called != (tc.wantID != 0) || w.Code != tc.wantStatus || w.Body.String() != tc.wantBody+"\n" {
				t.Fatalf("called = %v, status = %d, body = %s; want status %d and %s", called, w.Code, w.Body.String(), tc.wantStatus, tc.wantBody)
			}
			if w.Header().Get("Content-Type") != "application/json" {
				t.Fatal("playback response must be JSON")
			}
			if w.Header().Get("WWW-Authenticate") != "" {
				t.Fatal("an authenticated playback request must not challenge for credentials")
			}
			if tc.wantStatus == http.StatusInternalServerError && !strings.Contains(logs.String(), "get playback failed") {
				t.Fatal("playback failure must be logged")
			}
			output := logs.String() + w.Body.String()
			for _, private := range []string{"sensitive-database-detail", "SELECT stream_url", "partial-result", "playback-test-password", hash} {
				if strings.Contains(output, private) {
					t.Fatal("database details, partial result or credentials leaked")
				}
			}
		})
	}
}
