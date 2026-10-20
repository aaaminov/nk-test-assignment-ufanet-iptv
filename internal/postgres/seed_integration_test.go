//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"

	"nk-test-assignment-ufanet-iptv/internal/auth"
	"nk-test-assignment-ufanet-iptv/internal/postgres"
)

func TestSeed(t *testing.T) {
	pool := testPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Seed(ctx, pool); err != nil {
		t.Fatal(err)
	}

	for _, user := range []struct {
		login    string
		password string
		active   bool
	}{
		{"viewer", "viewer_demo", true},
		{"inactive", "inactive_demo", false},
	} {
		var hash string
		var active bool
		if err := pool.QueryRow(ctx, "SELECT password_hash, active FROM users WHERE login = $1", user.login).Scan(&hash, &active); err != nil {
			t.Fatal("read demo user failed")
		}
		if active != user.active || !auth.CheckPassword(hash, user.password) {
			t.Fatal("demo user activity or password is incorrect")
		}
		if cost, err := bcrypt.Cost([]byte(hash)); err != nil || cost != 10 {
			t.Fatal("demo password must be a bcrypt hash at cost 10")
		}
	}

	rows, err := pool.Query(ctx, "SELECT number, name, stream_url, active FROM channels ORDER BY number, id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count, activeCount := 0, 0
	for rows.Next() {
		var number int
		var name, streamURL string
		var active bool
		if err := rows.Scan(&number, &name, &streamURL, &active); err != nil {
			t.Fatal(err)
		}
		count++
		parsed, err := url.Parse(streamURL)
		if number != count || name == "" || err != nil || parsed.Scheme != "https" || parsed.Host == "" || !strings.HasSuffix(parsed.Path, ".m3u8") {
			t.Fatal("demo channel must have a positive number, name and absolute HLS URL")
		}
		if active {
			activeCount++
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if count != 5 || activeCount != 4 {
		t.Fatalf("demo channels: total = %d, active = %d; want 5 and 4", count, activeCount)
	}
	var subscriptions bool
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM subscriptions) = 5 AND
		(SELECT count(*) FROM data_seeds) = 2 AND
		EXISTS (SELECT 1 FROM data_seeds WHERE name = 'subscriptions_demo') AND
		(SELECT count(*) FROM subscriptions s
			JOIN users u ON u.id = s.user_id JOIN channels c ON c.id = s.channel_id
			WHERE u.login = 'viewer' AND CASE c.number
				WHEN 1 THEN s.active AND s.expires_at IS NULL
				WHEN 2 THEN s.active AND s.expires_at > statement_timestamp()
				WHEN 3 THEN NOT s.active AND s.expires_at IS NULL
				WHEN 4 THEN s.active AND s.expires_at < statement_timestamp()
				WHEN 5 THEN s.active AND s.expires_at IS NULL
				ELSE false END) = 5`).Scan(&subscriptions); err != nil || !subscriptions {
		t.Fatalf("demo subscription states and marker = %v, error = %v", subscriptions, err)
	}

	// Edit, delete and add data: a completed seed must leave all of it alone.
	if _, err := pool.Exec(ctx, `
		UPDATE users SET login = 'edited', password_hash = 'edited-hash', active = false WHERE login = 'viewer';
		DELETE FROM users WHERE login = 'inactive';
		INSERT INTO users (login, password_hash) VALUES ('custom', 'custom-hash');
		UPDATE channels SET number = 42, name = 'Edited', stream_url = 'https://example.com/edited.m3u8', active = false WHERE number = 1;
		DELETE FROM channels WHERE number = 5;
		INSERT INTO channels (number, name, stream_url) VALUES (99, 'Custom', 'https://example.com/custom.m3u8');
		UPDATE subscriptions SET active = false, expires_at = '2020-01-01T00:00:00Z'
			WHERE channel_id = (SELECT id FROM channels WHERE number = 42);
		DELETE FROM subscriptions WHERE channel_id = (SELECT id FROM channels WHERE number = 2);
		INSERT INTO subscriptions (user_id, channel_id, active)
			SELECT u.id, c.id, false FROM users u CROSS JOIN channels c
			WHERE u.login = 'custom' AND c.number = 99`); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Seed(ctx, pool); err != nil {
		t.Fatalf("repeat seed: %v", err)
	}
	var preserved bool
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM users) = 2 AND
		EXISTS (SELECT 1 FROM users WHERE login = 'edited' AND password_hash = 'edited-hash' AND NOT active) AND
		EXISTS (SELECT 1 FROM users WHERE login = 'custom' AND password_hash = 'custom-hash' AND active) AND
		(SELECT count(*) FROM channels) = 5 AND
		EXISTS (SELECT 1 FROM channels WHERE number = 42 AND name = 'Edited' AND stream_url = 'https://example.com/edited.m3u8' AND NOT active) AND
		EXISTS (SELECT 1 FROM channels WHERE number = 99 AND name = 'Custom') AND
		NOT EXISTS (SELECT 1 FROM channels WHERE number IN (1, 5)) AND
		(SELECT count(*) FROM subscriptions) = 4 AND
		EXISTS (SELECT 1 FROM subscriptions s JOIN channels c ON c.id = s.channel_id
			WHERE c.number = 42 AND NOT s.active AND s.expires_at = '2020-01-01T00:00:00Z') AND
		NOT EXISTS (SELECT 1 FROM subscriptions s JOIN channels c ON c.id = s.channel_id WHERE c.number = 2) AND
		EXISTS (SELECT 1 FROM subscriptions s JOIN users u ON u.id = s.user_id
			JOIN channels c ON c.id = s.channel_id WHERE u.login = 'custom' AND c.number = 99 AND NOT s.active) AND
		(SELECT count(*) FROM data_seeds) = 2 AND
		EXISTS (SELECT 1 FROM data_seeds WHERE name = 'subscriptions_demo') AND
		EXISTS (SELECT 1 FROM data_seeds WHERE name = 'base_demo')`).Scan(&preserved); err != nil || !preserved {
		t.Fatalf("repeat seed preserved data and marker = %v, error = %v", preserved, err)
	}
}

func TestSeedRollback(t *testing.T) {
	for _, table := range []string{"channels", "subscriptions", "data_seeds"} {
		t.Run(table+" write failure", func(t *testing.T) {
			pool := testPool(t)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			if err := postgres.Migrate(ctx, pool); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `
				INSERT INTO users (login, password_hash) VALUES ('custom', 'custom-hash');
				INSERT INTO channels (number, name, stream_url) VALUES (1, 'Custom', 'https://example.com/custom.m3u8')`); err != nil {
				t.Fatal(err)
			}
			check := "false"
			if table == "channels" {
				check = "number < 3"
			} else if table == "data_seeds" {
				check = "name <> 'subscriptions_demo'"
			}
			// Table names and constraints are test constants, never user input.
			if _, err := pool.Exec(ctx, "ALTER TABLE "+table+" ADD CONSTRAINT reject_seed CHECK ("+check+")"); err != nil {
				t.Fatal(err)
			}
			err := postgres.Seed(ctx, pool)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
				t.Fatal("seed must preserve the constraint violation")
			}
			if pool.Stat().AcquiredConns() != 0 {
				t.Fatal("failed seed did not release its connection")
			}
			var intact bool
			if err := pool.QueryRow(ctx, `SELECT
				(SELECT count(*) FROM users) = 1 AND
				EXISTS (SELECT 1 FROM users WHERE login = 'custom' AND password_hash = 'custom-hash' AND active) AND
				(SELECT count(*) FROM channels) = 1 AND
				EXISTS (SELECT 1 FROM channels WHERE number = 1 AND name = 'Custom' AND stream_url = 'https://example.com/custom.m3u8' AND active) AND
				(SELECT count(*) FROM subscriptions) = 0 AND
				(SELECT count(*) FROM data_seeds) = 0`).Scan(&intact); err != nil || !intact {
				t.Fatalf("rollback preserved original data = %v, error = %v", intact, err)
			}
			if _, err := pool.Exec(ctx, "ALTER TABLE "+table+" DROP CONSTRAINT reject_seed"); err != nil {
				t.Fatal(err)
			}
			if err := postgres.Seed(ctx, pool); err != nil {
				t.Fatalf("retry seed: %v", err)
			}
			var complete bool
			if err := pool.QueryRow(ctx, `SELECT
				(SELECT count(*) FROM users) = 3 AND
				(SELECT count(*) FROM channels) = 6 AND
				(SELECT count(*) FROM subscriptions) = 5 AND
				EXISTS (SELECT 1 FROM data_seeds WHERE name = 'base_demo') AND
				EXISTS (SELECT 1 FROM data_seeds WHERE name = 'subscriptions_demo')`).Scan(&complete); err != nil || !complete {
				t.Fatalf("retry created demo data and marker = %v, error = %v", complete, err)
			}
		})
	}
}

func TestSeedCancellation(t *testing.T) {
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
	if _, err := blocker.Exec(ctx, "LOCK TABLE channels IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	limited, cancelLimited := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancelLimited()
	if err := postgres.Seed(limited, pool); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked seed did not preserve deadline: %v", err)
	}
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var empty bool
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM users) = 0 AND
		(SELECT count(*) FROM channels) = 0 AND
		(SELECT count(*) FROM data_seeds) = 0`).Scan(&empty); err != nil || !empty {
		t.Fatalf("canceled seed left no data or marker = %v, error = %v", empty, err)
	}
	// Both pool slots must be usable after cancellation, even if pgx replaces a connection.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if err := postgres.Seed(ctx, pool); err != nil {
		t.Fatalf("retry after cancellation: %v", err)
	}
}
