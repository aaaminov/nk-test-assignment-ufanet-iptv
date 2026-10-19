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

func TestListActiveChannels(t *testing.T) {
	pool := testPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	fixtures := []struct {
		channel iptv.Channel
		active  bool
	}{
		{iptv.Channel{Number: 2, Name: "Later"}, true},
		{iptv.Channel{Number: 1, Name: "Первый"}, true},
		{iptv.Channel{Number: 1, Name: "Same number"}, true},
		{iptv.Channel{Number: 1, Name: "Disabled"}, false},
	}
	for i := range fixtures {
		f := &fixtures[i]
		if err := pool.QueryRow(ctx, `INSERT INTO channels (number, name, stream_url, active)
			VALUES ($1, $2, $3, $4) RETURNING id`,
			f.channel.Number, f.channel.Name, "https://stream.example.com/live/test/index.m3u8", f.active).Scan(&f.channel.ID); err != nil {
			t.Fatal("insert catalog fixture failed")
		}
	}

	repository := postgres.NewRepository(pool)
	got, err := repository.ListActiveChannels(ctx)
	want := []iptv.Channel{fixtures[1].channel, fixtures[2].channel, fixtures[0].channel}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("channels = %#v, error = %v; want %#v", got, err, want)
	}
	if pool.Stat().AcquiredConns() != 0 {
		t.Fatal("catalog did not release its connection")
	}

	if _, err := pool.Exec(ctx, "UPDATE channels SET active = false"); err != nil {
		t.Fatal(err)
	}
	got, err = repository.ListActiveChannels(ctx)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty catalog = %#v, error = %v", got, err)
	}
	if pool.Stat().AcquiredConns() != 0 {
		t.Fatal("empty catalog did not release its connection")
	}
}

func TestListActiveChannelsDatabaseFailure(t *testing.T) {
	pool := testPool(t)
	// A missing table must fail rather than look like an empty catalog.
	channels, err := postgres.NewRepository(pool).ListActiveChannels(t.Context())
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42P01" || channels != nil {
		t.Fatal("catalog must preserve the database failure and return no result")
	}
	if pool.Stat().AcquiredConns() != 0 {
		t.Fatal("failed catalog query did not release its connection")
	}
}

func TestListActiveChannelsCancellation(t *testing.T) {
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
	if _, err := repository.ListActiveChannels(canceled); !errors.Is(err, context.Canceled) {
		t.Fatal("catalog must preserve request cancellation")
	}
	limited, cancelLimited := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancelLimited()
	if _, err := repository.ListActiveChannels(limited); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("blocked catalog must honor the shorter request deadline")
	}
	if _, err := repository.ListActiveChannels(ctx); !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
		t.Fatal("blocked catalog must apply its own SQL timeout")
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
	if _, err := repository.ListActiveChannels(ctx); err != nil {
		t.Fatal("pool capacity was not restored after canceled catalog queries")
	}
}
