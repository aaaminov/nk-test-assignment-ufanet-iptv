//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"nk-test-assignment-ufanet-iptv/internal/iptv"
	"nk-test-assignment-ufanet-iptv/internal/postgres"
)

func TestSubscriptionQueries(t *testing.T) {
	pool := testPool(t)
	ctx := t.Context()
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, login, password_hash) OVERRIDING SYSTEM VALUE VALUES
			(1, 'first', 'test-hash'), (2, 'second', 'test-hash');
		INSERT INTO channels (id, number, name, stream_url, active) OVERRIDING SYSTEM VALUE VALUES
			(1, 2, 'Permanent', 'https://example.com/1.m3u8', true),
			(2, 1, 'Temporary', 'https://example.com/2.m3u8', true),
			(3, 3, 'Disabled subscription', 'https://example.com/3.m3u8', true),
			(4, 4, 'Expired', 'https://example.com/4.m3u8', true),
			(5, 5, 'Disabled channel', 'https://example.com/5.m3u8', false),
			(6, 6, 'Other user', 'https://example.com/6.m3u8', true),
			(7, 2, 'Same number', 'https://example.com/7.m3u8', true);
		INSERT INTO subscriptions (user_id, channel_id, active, expires_at) VALUES
			(1, 1, true, NULL),
			(1, 2, true, statement_timestamp() + interval '1 day'),
			(1, 3, false, NULL),
			(1, 4, true, statement_timestamp() - interval '1 day'),
			(1, 5, true, NULL),
			(1, 7, true, NULL),
			(2, 6, true, NULL)`); err != nil {
		t.Fatal(err)
	}
	repository := postgres.NewRepository(pool)
	got, err := repository.ListSubscribedChannels(ctx, 1)
	want := []iptv.Channel{{ID: 2, Number: 1, Name: "Temporary"}, {ID: 1, Number: 2, Name: "Permanent"}, {ID: 7, Number: 2, Name: "Same number"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("subscribed channels = %#v, error = %v; want %#v", got, err, want)
	}
	got, err = repository.ListSubscribedChannels(ctx, 2)
	if err != nil || !reflect.DeepEqual(got, []iptv.Channel{{ID: 6, Number: 6, Name: "Other user"}}) {
		t.Fatalf("other user's channels = %#v, error = %v", got, err)
	}
	got, err = repository.ListSubscribedChannels(ctx, 99)
	if err != nil || len(got) != 0 {
		t.Fatalf("unsubscribed user's catalog = %#v, error = %v", got, err)
	}

	for _, tc := range []struct {
		name      string
		userID    int64
		channelID int64
		allowed   bool
		wantErr   error
	}{
		{"permanent", 1, 1, true, nil},
		{"temporary", 1, 2, true, nil},
		{"disabled subscription", 1, 3, false, nil},
		{"expired", 1, 4, false, nil},
		{"disabled channel", 1, 5, false, iptv.ErrChannelNotFound},
		{"no subscription", 1, 6, false, nil},
		{"other user denied", 2, 1, false, nil},
		{"other user allowed", 2, 6, true, nil},
		{"missing channel", 1, 99, false, iptv.ErrChannelNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			access, err := repository.LookupPlaybackAccess(ctx, tc.userID, tc.channelID)
			if !errors.Is(err, tc.wantErr) || access.Allowed != tc.allowed {
				t.Fatalf("access = %#v, error = %v; want allowed %v, error %v", access, err, tc.allowed, tc.wantErr)
			}
			if tc.wantErr != nil {
				if access != (iptv.PlaybackAccess{}) {
					t.Fatal("failed lookup returned a partial result")
				}
			} else if access.Playback.ChannelID != tc.channelID || access.Playback.URL == "" {
				t.Fatal("active channel must be returned even without permission")
			}
		})
	}
	if pool.Stat().AcquiredConns() != 0 {
		t.Fatal("subscription queries did not release their connections")
	}
	// Deleting either parent removes only its own subscriptions.
	if _, err := pool.Exec(ctx, "DELETE FROM channels WHERE id = 1; DELETE FROM users WHERE id = 2"); err != nil {
		t.Fatal(err)
	}
	var intact bool
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM subscriptions) = 5
		AND NOT EXISTS (SELECT 1 FROM subscriptions WHERE channel_id = 1 OR user_id = 2)`).Scan(&intact); err != nil || !intact {
		t.Fatalf("parent deletion removed related subscriptions = %v, error = %v", intact, err)
	}
}

func TestSubscriptionExpirationBoundary(t *testing.T) {
	pool := testPool(t)
	ctx := t.Context()
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	// A view supplies expiry at statement time +/- one microsecond. Both real
	// queries see exact boundary values without sleeps or an application clock.
	if _, err := pool.Exec(ctx, `
		INSERT INTO channels (number, name, stream_url)
			SELECT n, 'Boundary', 'https://example.com/boundary.m3u8' FROM generate_series(1, 3) n;
		DROP TABLE subscriptions;
		CREATE VIEW subscriptions AS SELECT 1::bigint AS user_id, id AS channel_id, true AS active,
			statement_timestamp() + (number - 2) * interval '1 microsecond' AS expires_at
			FROM channels`); err != nil {
		t.Fatal(err)
	}
	repository := postgres.NewRepository(pool)
	channels, err := repository.ListSubscribedChannels(ctx, 1)
	if err != nil || len(channels) != 1 || channels[0].Number != 3 {
		t.Fatalf("only expiry strictly after statement time must be listed: %#v, error = %v", channels, err)
	}
	for id := int64(1); id <= 3; id++ {
		access, err := repository.LookupPlaybackAccess(ctx, 1, id)
		if err != nil || access.Allowed != (id == 3) {
			t.Fatalf("boundary channel %d: access = %#v, error = %v", id, access, err)
		}
	}
}

func TestSubscriptionQueriesDatabaseFailure(t *testing.T) {
	pool := testPool(t)
	repository := postgres.NewRepository(pool)
	channels, err := repository.ListSubscribedChannels(t.Context(), 1)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42P01" || channels != nil {
		t.Fatal("subscribed catalog must preserve the database failure and return no result")
	}
	access, err := repository.LookupPlaybackAccess(t.Context(), 1, 1)
	if !errors.As(err, &pgErr) || pgErr.Code != "42P01" || access != (iptv.PlaybackAccess{}) {
		t.Fatal("playback access must preserve the database failure and return no result")
	}
	if pool.Stat().AcquiredConns() != 0 {
		t.Fatal("failed subscription queries did not release their connections")
	}
}

func TestSubscriptionQueriesCancellation(t *testing.T) {
	pool := testPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	if _, err := blocker.Exec(ctx, "LOCK TABLE subscriptions IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	repository := postgres.NewRepository(pool)
	for _, query := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"catalog", func(ctx context.Context) error { _, err := repository.ListSubscribedChannels(ctx, 1); return err }},
		{"playback", func(ctx context.Context) error { _, err := repository.LookupPlaybackAccess(ctx, 1, 1); return err }},
	} {
		t.Run(query.name, func(t *testing.T) {
			canceled, cancelRequest := context.WithCancel(ctx)
			cancelRequest()
			if err := query.run(canceled); !errors.Is(err, context.Canceled) {
				t.Fatal("subscription query must preserve request cancellation")
			}
			limited, cancelLimited := context.WithTimeout(ctx, 100*time.Millisecond)
			defer cancelLimited()
			if err := query.run(limited); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("blocked subscription query must honor the shorter deadline")
			}
			if err := query.run(ctx); !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				t.Fatal("blocked subscription query must apply its own SQL timeout")
			}
		})
	}
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := repository.ListSubscribedChannels(ctx, 1); err != nil {
		t.Fatalf("catalog pool capacity was not restored: %v", err)
	}
	if _, err := repository.LookupPlaybackAccess(ctx, 1, 1); !errors.Is(err, iptv.ErrChannelNotFound) {
		t.Fatalf("playback pool capacity was not restored: %v", err)
	}
}
