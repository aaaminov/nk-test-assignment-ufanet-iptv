//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"nk-test-assignment-ufanet-iptv/internal/iptv"
	"nk-test-assignment-ufanet-iptv/internal/postgres"
)

func TestLookupUser(t *testing.T) {
	pool := testPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	const specialLogin = "viewer'; DROP TABLE users; --"
	fixtures := []struct {
		login string
		user  iptv.User
	}{
		{"viewer", iptv.User{PasswordHash: "active-test-hash", Active: true}},
		{"inactive", iptv.User{PasswordHash: "inactive-test-hash", Active: false}},
		{specialLogin, iptv.User{PasswordHash: "literal-test-hash", Active: true}},
	}
	for i := range fixtures {
		f := &fixtures[i]
		if err := pool.QueryRow(ctx, `INSERT INTO users (login, password_hash, active)
			VALUES ($1, $2, $3) RETURNING id`, f.login, f.user.PasswordHash, f.user.Active).Scan(&f.user.ID); err != nil {
			t.Fatal("insert lookup fixture failed")
		}
	}
	repository := postgres.NewRepository(pool)
	for _, tc := range []struct {
		name  string
		login string
		want  iptv.User
	}{
		{"active", fixtures[0].login, fixtures[0].user},
		{"inactive", fixtures[1].login, fixtures[1].user},
		{"unknown", "unknown", iptv.User{}},
		{"SQL injection", "' OR true --", iptv.User{}},
		{"literal SQL metacharacters", specialLogin, fixtures[2].user},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := repository.LookupUser(ctx, tc.login)
			if tc.want.ID == 0 {
				if !errors.Is(err, iptv.ErrUserNotFound) || got != (iptv.User{}) {
					t.Fatal("unknown login must return ErrUserNotFound and no user")
				}
			} else if err != nil || got != tc.want {
				t.Fatal("lookup did not return the stored user")
			}
			if pool.Stat().AcquiredConns() != 0 {
				t.Fatal("lookup did not release its connection")
			}
		})
	}
}

func TestLookupUserDatabaseFailure(t *testing.T) {
	pool := testPool(t)
	// No migrations: the missing table must be a failure, not an unknown user.
	user, err := postgres.NewRepository(pool).LookupUser(t.Context(), "viewer")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42P01" || errors.Is(err, iptv.ErrUserNotFound) {
		t.Fatal("lookup must preserve the database failure separately from ErrUserNotFound")
	}
	if user != (iptv.User{}) || pool.Stat().AcquiredConns() != 0 {
		t.Fatal("failed lookup must return no user and release its connection")
	}
}

func TestLookupUserCancellation(t *testing.T) {
	pool := testPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	if _, err := blocker.Exec(ctx, "LOCK TABLE users IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	repository := postgres.NewRepository(pool)

	canceled, cancelRequest := context.WithCancel(ctx)
	cancelRequest()
	if _, err := repository.LookupUser(canceled, "viewer"); !errors.Is(err, context.Canceled) {
		t.Fatal("lookup must preserve request cancellation")
	}

	limited, cancelLimited := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancelLimited()
	if _, err := repository.LookupUser(limited, "viewer"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("blocked lookup must honor the shorter request deadline")
	}
	if _, err := repository.LookupUser(ctx, "viewer"); !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
		t.Fatal("blocked lookup must apply its own SQL timeout before the parent deadline")
	}
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	// Hold one slot: a subsequent lookup must be able to use the other after cancellation.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := repository.LookupUser(ctx, "unknown"); !errors.Is(err, iptv.ErrUserNotFound) {
		t.Fatal("pool capacity was not restored after canceled lookups")
	}
}
