//go:build integration

package httpapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"nk-test-assignment-ufanet-iptv/internal/httpapi"
	"nk-test-assignment-ufanet-iptv/internal/iptv"
	"nk-test-assignment-ufanet-iptv/internal/postgres"
	"nk-test-assignment-ufanet-iptv/internal/testdb"
)

func TestSubscriptionsPostgreSQL(t *testing.T) {
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
	// Public demo credentials are shared; subscription ownership still differs.
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO users (login, password_hash) SELECT 'other', password_hash FROM users WHERE login = 'viewer';
		INSERT INTO subscriptions (user_id, channel_id) SELECT id, 1 FROM users WHERE login = 'inactive'`); err != nil {
		t.Fatal(err)
	}
	repository := postgres.NewRepository(pool)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	server := httptest.NewServer(httpapi.NewRouter(repository, iptv.NewCatalog(repository), iptv.NewPlayer(repository), logger))
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 5 * time.Second

	// Changes are cumulative and take effect on the next request to either resource.
	for _, tc := range []struct {
		name         string
		change       string
		login        string
		channelID    int64
		wantChannels []int64
		wantPlayback int
	}{
		{"permanent subscription", "", "viewer", 1, []int64{1, 2}, http.StatusOK},
		{"no subscriptions or identity override", "", "other", 1, []int64{}, http.StatusForbidden},
		{"another user's catalog", `INSERT INTO subscriptions (user_id, channel_id) SELECT id, 3 FROM users WHERE login = 'other'`, "other", 3, []int64{3}, http.StatusOK},
		{"other user's subscription grants no access", "", "viewer", 3, []int64{1, 2}, http.StatusForbidden},
		{"disable subscription", `UPDATE subscriptions SET active = false WHERE user_id = 1 AND channel_id = 1`, "viewer", 1, []int64{2}, http.StatusForbidden},
		{"enable subscription", `UPDATE subscriptions SET active = true WHERE user_id = 1 AND channel_id = 1`, "viewer", 1, []int64{1, 2}, http.StatusOK},
		{"expire temporary subscription", `UPDATE subscriptions SET expires_at = statement_timestamp() - interval '1 microsecond' WHERE user_id = 1 AND channel_id = 2`, "viewer", 2, []int64{1}, http.StatusForbidden},
		{"renew temporary subscription", `UPDATE subscriptions SET expires_at = statement_timestamp() + interval '1 day' WHERE user_id = 1 AND channel_id = 2`, "viewer", 2, []int64{1, 2}, http.StatusOK},
		{"disable channel with subscription", `UPDATE channels SET active = false WHERE id = 1`, "viewer", 1, []int64{2}, http.StatusNotFound},
		{"delete subscription", `UPDATE channels SET active = true WHERE id = 1; DELETE FROM subscriptions WHERE user_id = 1 AND channel_id = 1`, "viewer", 1, []int64{2}, http.StatusForbidden},
		{"empty catalog", `UPDATE subscriptions SET active = false WHERE user_id = 1`, "viewer", 2, []int64{}, http.StatusForbidden},
		{"missing channel without subscription", "", "viewer", 99, []int64{}, http.StatusNotFound},
		{"inactive user with subscription", "", "inactive", 1, nil, http.StatusUnauthorized},
		{"enable user with subscription", `UPDATE users SET active = true WHERE login = 'inactive'`, "inactive", 1, []int64{1}, http.StatusOK},
		{"disable user with subscription", `UPDATE users SET active = false WHERE login = 'inactive'`, "inactive", 1, nil, http.StatusUnauthorized},
		{"subscription database failure", `DROP TABLE subscriptions`, "viewer", 1, nil, http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.change != "" {
				if _, err := pool.Exec(t.Context(), tc.change); err != nil {
					t.Fatal(err)
				}
			}
			for _, path := range []string{"/api/v1/channels", fmt.Sprintf("/api/v1/channels/%d/playback", tc.channelID)} {
				req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+path+"?user_id=1", nil)
				if err != nil {
					t.Fatal(err)
				}
				password := "viewer_demo"
				if tc.login == "inactive" {
					password = "inactive_demo"
				}
				req.SetBasicAuth(tc.login, password)
				req.Header.Set("X-User-ID", "1")
				response, err := client.Do(req)
				if err != nil {
					t.Fatal("HTTP subscription request failed")
				}
				body, readErr := io.ReadAll(response.Body)
				closeErr := response.Body.Close()
				if readErr != nil || closeErr != nil {
					t.Fatalf("read body: %v; close body: %v", readErr, closeErr)
				}
				wantStatus := tc.wantPlayback
				if path == "/api/v1/channels" && wantStatus != http.StatusUnauthorized && wantStatus != http.StatusInternalServerError {
					wantStatus = http.StatusOK
				}
				if response.StatusCode != wantStatus || response.Header.Get("Content-Type") != "application/json" {
					t.Fatalf("%s: status = %d, body = %s; want status %d", path, response.StatusCode, body, wantStatus)
				}
				wantChallenge := ""
				if wantStatus == http.StatusUnauthorized {
					wantChallenge = `Basic realm="iptv"`
				}
				if response.Header.Get("WWW-Authenticate") != wantChallenge {
					t.Fatal("only failed authentication must include a Basic Auth challenge")
				}
				switch {
				case wantStatus == http.StatusUnauthorized:
					if strings.TrimSpace(string(body)) != `{"error":{"code":"unauthorized","message":"authentication required"}}` {
						t.Fatal("inactive user must receive the uniform JSON 401")
					}
				case wantStatus == http.StatusInternalServerError:
					if strings.TrimSpace(string(body)) != `{"error":{"code":"internal_error","message":"internal server error"}}` {
						t.Fatal("a subscription lookup failure must return a safe JSON 500")
					}
				case path == "/api/v1/channels":
					var result struct {
						Channels []struct{ ID int64 }
					}
					if err := json.Unmarshal(body, &result); err != nil || result.Channels == nil {
						t.Fatal("catalog must return a JSON array, including when empty")
					}
					ids := make([]int64, 0, len(result.Channels))
					for _, channel := range result.Channels {
						ids = append(ids, channel.ID)
					}
					if !reflect.DeepEqual(ids, tc.wantChannels) {
						t.Fatalf("catalog IDs = %v; want %v", ids, tc.wantChannels)
					}
				case wantStatus == http.StatusOK:
					wantBody := fmt.Sprintf(`{"channel_id":%d,"url":"https://stream.example.com/live/ch%d/index.m3u8"}`, tc.channelID, tc.channelID)
					if strings.TrimSpace(string(body)) != wantBody {
						t.Fatalf("playback = %s; want %s", body, wantBody)
					}
				default:
					wantBody := `{"error":{"code":"subscription_required","message":"active subscription required"}}`
					if wantStatus == http.StatusNotFound {
						wantBody = `{"error":{"code":"channel_not_found","message":"channel not found"}}`
					}
					if strings.TrimSpace(string(body)) != wantBody {
						t.Fatalf("denied playback = %s; want %s", body, wantBody)
					}
				}
				if pool.Stat().AcquiredConns() != 0 {
					t.Fatal("HTTP subscription request did not release its connections")
				}
			}
		})
	}
	for _, operation := range []string{"list channels failed", "get playback failed"} {
		if !strings.Contains(logs.String(), operation) {
			t.Fatal("subscription database failure must be logged for both resources")
		}
	}
	for _, private := range []string{"SELECT", "SQLSTATE", "42P01", "viewer_demo", "inactive_demo", "postgres://", "password_hash"} {
		if strings.Contains(logs.String(), private) {
			t.Fatal("database details or credentials leaked into subscription logs")
		}
	}
}
