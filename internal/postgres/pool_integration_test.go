//go:build integration

package postgres_test

import (
	"context"
	"crypto/rand"
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"nk-test-assignment-ufanet-iptv/internal/postgres"
)

func TestOpen(t *testing.T) {
	databaseURL, schema := isolatedDatabase(t)

	t.Run("connection and pool limit", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		pool, err := postgres.Open(ctx, databaseURL, 2)
		if err != nil {
			t.Fatal("open test pool failed")
		}
		t.Cleanup(pool.Close)

		// Hold both connections to verify that every connection uses the isolated schema.
		for range 2 {
			conn, err := pool.Acquire(ctx)
			if err != nil {
				t.Fatal("acquire test connection failed")
			}
			defer conn.Release()
			var currentSchema string
			if err := conn.QueryRow(ctx, "SELECT current_schema()").Scan(&currentSchema); err != nil {
				t.Fatal("query test schema failed")
			}
			if currentSchema != schema {
				t.Fatal("connection is outside the isolated schema")
			}
		}

		limited, cancelLimited := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancelLimited()
		conn, err := pool.Acquire(limited)
		if conn != nil {
			conn.Release()
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("pool did not enforce DB_MAX_CONNS")
		}
	})

	t.Run("canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		pool, err := postgres.Open(ctx, databaseURL, 2)
		if pool != nil {
			pool.Close()
			t.Fatal("Open returned a pool after cancellation")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatal("Open did not preserve context cancellation")
		}
	})

	t.Run("connection timeout", func(t *testing.T) {
		// A silent TCP peer accepts the connection but never completes the PostgreSQL handshake.
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		started := time.Now()
		pool, err := postgres.Open(t.Context(), "postgres://iptv_test@"+listener.Addr().String()+"/iptv_test?sslmode=disable&connect_timeout=30", 2)
		if pool != nil {
			pool.Close()
			t.Fatal("Open returned a pool without a completed handshake")
		}
		if err == nil || time.Since(started) > 5*time.Second {
			t.Fatal("Open did not bound the connection attempt and cleanup")
		}
	})

	t.Run("unavailable database", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := listener.Addr().String()
		if err := listener.Close(); err != nil {
			t.Fatal(err)
		}
		pool, err := postgres.Open(t.Context(), "postgres://iptv_test@"+addr+"/iptv_test?sslmode=disable", 2)
		if pool != nil {
			pool.Close()
			t.Fatal("Open returned a pool for an unavailable database")
		}
		if err == nil {
			t.Fatal("Open succeeded for an unavailable database")
		}
	})
}

// isolatedDatabase creates only its own schema in the explicitly selected test database.
func isolatedDatabase(t *testing.T) (databaseURL, schema string) {
	t.Helper()
	databaseURL = os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("TEST_DATABASE_URL is required (use a separate test database)")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatal("TEST_DATABASE_URL must be a PostgreSQL URL")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal("connect test database failed (check TEST_DATABASE_URL)")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := admin.Close(ctx); err != nil {
			t.Error("close test database connection failed")
		}
	})

	schema = "iptv_test_" + strings.ToLower(rand.Text())
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal("create test schema failed")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Error("drop test schema failed")
		}
	})

	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String(), schema
}
