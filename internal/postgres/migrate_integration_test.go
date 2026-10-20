//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"nk-test-assignment-ufanet-iptv/internal/postgres"
	"nk-test-assignment-ufanet-iptv/internal/testdb"
)

func TestMigrate(t *testing.T) {
	pool := testPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var empty bool
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM users) = 0 AND
		(SELECT count(*) FROM channels) = 0 AND
		(SELECT count(*) FROM subscriptions) = 0 AND
		(SELECT count(*) FROM schema_migrations) = 3 AND
		EXISTS (SELECT 1 FROM schema_migrations WHERE version = '001_initial.sql') AND
		EXISTS (SELECT 1 FROM schema_migrations WHERE version = '002_seed.sql') AND
		EXISTS (SELECT 1 FROM schema_migrations WHERE version = '003_subscriptions.sql') AND
		(SELECT count(*) FROM data_seeds) = 0`).Scan(&empty); err != nil || !empty {
		t.Fatalf("initial schema: empty tables and three versions = %v, error = %v", empty, err)
	}

	var userID, channelID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users (login, password_hash, active)
		VALUES ('viewer', 'test-hash', false) RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO channels (number, name, stream_url)
		VALUES (1, 'Original', 'https://stream.example.com/live/1/index.m3u8')
		RETURNING id`).Scan(&channelID); err != nil {
		t.Fatal(err)
	}
	if userID <= 0 || channelID <= 0 {
		t.Fatal("generated IDs must be positive")
	}
	if _, err := pool.Exec(ctx, "INSERT INTO subscriptions (user_id, channel_id) VALUES ($1, $2)", userID, channelID); err != nil {
		t.Fatal(err)
	}
	var permanent bool
	if err := pool.QueryRow(ctx, "SELECT active AND expires_at IS NULL FROM subscriptions WHERE user_id = $1 AND channel_id = $2", userID, channelID).Scan(&permanent); err != nil || !permanent {
		t.Fatalf("subscription defaults: permanent and active = %v, error = %v", permanent, err)
	}
	var active bool
	if err := pool.QueryRow(ctx, "SELECT active FROM channels WHERE id = $1", channelID).Scan(&active); err != nil || !active {
		t.Fatalf("channel active default: got %v, error = %v", active, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE channels SET name = 'Edited', active = false WHERE id = $1`, channelID); err != nil {
		t.Fatal(err)
	}
	// Equal channel numbers are allowed; the catalog will use id to break ties.
	if _, err := pool.Exec(ctx, `INSERT INTO channels (number, name, stream_url)
		VALUES (1, 'Another', 'https://stream.example.com/live/2/index.m3u8')`); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		sql  string
		code string
	}{
		{"duplicate login", "INSERT INTO users (login, password_hash) VALUES ('viewer', 'other-hash')", "23505"},
		{"invalid number", "INSERT INTO channels (number, name, stream_url) VALUES (0, 'Invalid', 'https://stream.example.com/index.m3u8')", "23514"},
		{"duplicate subscription", "INSERT INTO subscriptions (user_id, channel_id) SELECT user_id, channel_id FROM subscriptions", "23505"},
		{"missing user", "INSERT INTO subscriptions (user_id, channel_id) SELECT -1, id FROM channels LIMIT 1", "23503"},
		{"missing channel", "INSERT INTO subscriptions (user_id, channel_id) SELECT id, -1 FROM users LIMIT 1", "23503"},
		{"null activity", "UPDATE subscriptions SET active = NULL", "23502"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, tc.sql)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != tc.code {
				t.Fatalf("constraint error: got %v, want SQLSTATE %s", err, tc.code)
			}
		})
	}

	// Recreate the state before 002_seed.sql to verify an upgrade preserves data.
	if _, err := pool.Exec(ctx, "DROP TABLE data_seeds; DELETE FROM schema_migrations WHERE version = '002_seed.sql'"); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade migration: %v", err)
	}
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	var preserved bool
	if err := pool.QueryRow(ctx, `SELECT
		EXISTS (SELECT 1 FROM users WHERE id = $1 AND login = 'viewer' AND password_hash = 'test-hash' AND NOT active) AND
		EXISTS (SELECT 1 FROM channels WHERE id = $2 AND number = 1 AND name = 'Edited' AND NOT active
			AND stream_url = 'https://stream.example.com/live/1/index.m3u8') AND
		(SELECT count(*) FROM users) = 1 AND
		(SELECT count(*) FROM channels) = 2 AND
		(SELECT count(*) FROM subscriptions) = 1 AND
		(SELECT count(*) FROM schema_migrations) = 3`, userID, channelID).Scan(&preserved); err != nil || !preserved {
		t.Fatalf("repeat migration preserved data and version: %v, error = %v", preserved, err)
	}
}

func TestMigrateRollback(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setup   string
		code    string
		intact  string
		cleanup string
	}{
		{
			name:    "DDL failure",
			setup:   "CREATE TABLE channels (marker integer); INSERT INTO channels VALUES (7)",
			code:    "42P07",
			intact:  "SELECT to_regclass('users') IS NULL AND to_regclass('schema_migrations') IS NULL AND (SELECT marker FROM channels) = 7",
			cleanup: "DROP TABLE channels",
		},
		{
			name:    "subscriptions DDL failure",
			setup:   "CREATE TABLE subscriptions (marker integer); INSERT INTO subscriptions VALUES (7)",
			code:    "42P07",
			intact:  "SELECT to_regclass('users') IS NULL AND to_regclass('data_seeds') IS NULL AND to_regclass('schema_migrations') IS NULL AND (SELECT marker FROM subscriptions) = 7",
			cleanup: "DROP TABLE subscriptions",
		},
		{
			name:    "version write failure",
			setup:   "CREATE TABLE schema_migrations (version text PRIMARY KEY CHECK (false))",
			code:    "23514",
			intact:  "SELECT to_regclass('users') IS NULL AND to_regclass('channels') IS NULL AND (SELECT count(*) FROM schema_migrations) = 0",
			cleanup: "DROP TABLE schema_migrations",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := testPool(t)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			if _, err := pool.Exec(ctx, tc.setup); err != nil {
				t.Fatal(err)
			}
			err := postgres.Migrate(ctx, pool)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != tc.code {
				t.Fatalf("migration error: got %v, want SQLSTATE %s", err, tc.code)
			}
			var intact bool
			if err := pool.QueryRow(ctx, tc.intact).Scan(&intact); err != nil || !intact {
				t.Fatalf("rollback preserved original schema: %v, error = %v", intact, err)
			}
			if pool.Stat().AcquiredConns() != 0 {
				t.Fatal("failed migration did not release its connection")
			}
			if _, err := pool.Exec(ctx, tc.cleanup); err != nil {
				t.Fatal(err)
			}
			if err := postgres.Migrate(ctx, pool); err != nil {
				t.Fatalf("retry migration: %v", err)
			}
			var complete bool
			if err := pool.QueryRow(ctx, `SELECT
				to_regclass('users') IS NOT NULL AND to_regclass('channels') IS NOT NULL AND
				to_regclass('subscriptions') IS NOT NULL AND
				(SELECT count(*) FROM schema_migrations) = 3`).Scan(&complete); err != nil || !complete {
				t.Fatalf("retry created schema and version: %v, error = %v", complete, err)
			}
		})
	}
}

func TestSubscriptionsUpgrade(t *testing.T) {
	pool := testPool(t)
	ctx := t.Context()
	// Build the previously shipped schema using its unchanged migration files.
	if _, err := pool.Exec(ctx, "CREATE TABLE schema_migrations (version text PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"001_initial.sql", "002_seed.sql"} {
		sql, err := os.ReadFile("migrations/" + version)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, "INSERT INTO schema_migrations VALUES ($1)", version); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO data_seeds VALUES ('base_demo');
		INSERT INTO users (login, password_hash, active) VALUES ('viewer', 'edited-hash', false);
		INSERT INTO channels (number, name, stream_url, active) VALUES
			(1, 'Edited', 'https://stream.example.com/live/ch1/index.m3u8', false),
			(2, 'Custom', 'https://example.com/custom.m3u8', true),
			(3, 'Existing demo', 'https://stream.example.com/live/ch3/index.m3u8', true)`); err != nil {
		t.Fatal(err)
	}
	for attempt := range 2 {
		if err := postgres.Migrate(ctx, pool); err != nil {
			t.Fatal(err)
		}
		if attempt == 0 {
			if _, err := pool.Exec(ctx, `INSERT INTO subscriptions (user_id, channel_id, active, expires_at)
				SELECT u.id, c.id, false, '2020-01-01T00:00:00Z'
				FROM users u CROSS JOIN channels c WHERE c.number = 1`); err != nil {
				t.Fatal(err)
			}
		}
		if err := postgres.Seed(ctx, pool); err != nil {
			t.Fatal(err)
		}
	}
	var preserved bool
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM schema_migrations) = 3 AND
		(SELECT count(*) FROM data_seeds) = 2 AND
		EXISTS (SELECT 1 FROM data_seeds WHERE name = 'subscriptions_demo') AND
		(SELECT count(*) FROM users) = 1 AND
		EXISTS (SELECT 1 FROM users WHERE login = 'viewer' AND password_hash = 'edited-hash' AND NOT active) AND
		(SELECT count(*) FROM channels) = 3 AND
		EXISTS (SELECT 1 FROM channels WHERE name = 'Edited' AND NOT active) AND
		EXISTS (SELECT 1 FROM channels WHERE name = 'Custom' AND stream_url = 'https://example.com/custom.m3u8') AND
		(SELECT count(*) FROM subscriptions) = 2 AND
		EXISTS (SELECT 1 FROM subscriptions s JOIN channels c ON c.id = s.channel_id
			WHERE c.number = 3 AND NOT s.active AND s.expires_at IS NULL) AND
		EXISTS (SELECT 1 FROM subscriptions s JOIN channels c ON c.id = s.channel_id
			WHERE c.number = 1 AND NOT s.active AND s.expires_at = '2020-01-01T00:00:00Z')`).Scan(&preserved); err != nil || !preserved {
		t.Fatalf("upgrade preserved edits and seeded existing demo records once = %v, error = %v", preserved, err)
	}
}

func TestMigrateCancellation(t *testing.T) {
	pool := testPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	if _, err := blocker.Exec(ctx, "LOCK TABLE schema_migrations IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	limited, cancelLimited := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancelLimited()
	if err := postgres.Migrate(limited, pool); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked migration did not preserve deadline: %v", err)
	}
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	// Hold one connection while retrying: both pool slots must remain usable
	// after the canceled connection has been released or replaced.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("retry after cancellation: %v", err)
	}
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL, _ := testdb.IsolatedDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	pool, err := postgres.Open(ctx, databaseURL, 2)
	if err != nil {
		t.Fatal("open migration test pool failed")
	}
	t.Cleanup(pool.Close)
	return pool
}
