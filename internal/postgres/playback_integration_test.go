//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"nk-test-assignment-ufanet-iptv/internal/iptv"
	"nk-test-assignment-ufanet-iptv/internal/postgres"
)

func TestLookupActivePlayback(t *testing.T) {
	pool := testPool(t)
	if err := postgres.Migrate(t.Context(), pool); err != nil {
		t.Fatal(err)
	}
	const streamURL = "https://stream.example.com/live/custom/index.m3u8?quality=hd"
	var id int64
	if err := pool.QueryRow(t.Context(), `INSERT INTO channels (number, name, stream_url)
		VALUES (1, 'Playback test', $1) RETURNING id`, streamURL).Scan(&id); err != nil {
		t.Fatal("insert playback fixture failed")
	}
	repository := postgres.NewRepository(pool)
	got, err := repository.LookupActivePlayback(t.Context(), id)
	want := iptv.Playback{ChannelID: id, URL: streamURL}
	if err != nil || got != want {
		t.Fatalf("playback = %#v, error = %v; want %#v", got, err, want)
	}
	if pool.Stat().AcquiredConns() != 0 {
		t.Fatal("playback lookup did not release its connection")
	}

	if _, err := pool.Exec(t.Context(), "UPDATE channels SET active = false WHERE id = $1", id); err != nil {
		t.Fatal(err)
	}
	for _, channelID := range []int64{id, math.MaxInt64} {
		got, err := repository.LookupActivePlayback(t.Context(), channelID)
		if !errors.Is(err, iptv.ErrChannelNotFound) || got != (iptv.Playback{}) {
			t.Fatal("disabled and missing channels must return no playback and ErrChannelNotFound")
		}
		if pool.Stat().AcquiredConns() != 0 {
			t.Fatal("unavailable playback lookup did not release its connection")
		}
	}
}

func TestLookupActivePlaybackDatabaseFailure(t *testing.T) {
	pool := testPool(t)
	// A missing table must fail rather than look like an unavailable channel.
	got, err := postgres.NewRepository(pool).LookupActivePlayback(t.Context(), 1)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42P01" || got != (iptv.Playback{}) {
		t.Fatal("playback must preserve the database failure and return no result")
	}
	if pool.Stat().AcquiredConns() != 0 {
		t.Fatal("failed playback lookup did not release its connection")
	}
}

func TestLookupActivePlaybackCancellation(t *testing.T) {
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
	if _, err := blocker.Exec(ctx, "LOCK TABLE channels IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	repository := postgres.NewRepository(pool)

	canceled, cancelRequest := context.WithCancel(ctx)
	cancelRequest()
	if _, err := repository.LookupActivePlayback(canceled, 1); !errors.Is(err, context.Canceled) {
		t.Fatal("playback must preserve request cancellation")
	}
	limited, cancelLimited := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancelLimited()
	if _, err := repository.LookupActivePlayback(limited, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("blocked playback must honor the shorter request deadline")
	}
	if _, err := repository.LookupActivePlayback(ctx, 1); !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
		t.Fatal("blocked playback must apply its own SQL timeout")
	}
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	// Hold one slot: the other must be available after canceled queries.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := repository.LookupActivePlayback(ctx, 1); !errors.Is(err, iptv.ErrChannelNotFound) {
		t.Fatal("pool capacity was not restored after canceled playback lookups")
	}
}
