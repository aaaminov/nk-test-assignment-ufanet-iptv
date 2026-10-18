package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"nk-test-assignment-ufanet-iptv/internal/auth"
)

// Seed creates public demo data once, after migrations and before HTTP starts.
// Data and the completion marker commit together; repeat calls preserve edits.
// Only one app instance is supported. Errors must not be logged verbatim.
func Seed(ctx context.Context, pool *pgxpool.Pool) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin seed: %w", err)
	}
	defer tx.Rollback(ctx)

	const seed = "base_demo"
	var applied bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM data_seeds WHERE name = $1)", seed).Scan(&applied); err != nil {
		return fmt.Errorf("check seed: %w", err)
	}
	if applied {
		return nil
	}

	for _, user := range []struct {
		login    string
		password string
		active   bool
	}{
		{"viewer", "viewer_demo", true},
		{"inactive", "inactive_demo", false},
	} {
		hash, err := auth.HashPassword(user.password)
		if err != nil {
			return fmt.Errorf("hash demo password: %w", err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO users (login, password_hash, active) VALUES ($1, $2, $3)", user.login, hash, user.active); err != nil {
			return fmt.Errorf("seed users: %w", err)
		}
	}

	for i, name := range []string{"Первый", "Россия 1", "Матч ТВ", "НТВ", "Культура"} {
		number := i + 1
		url := fmt.Sprintf("https://stream.example.com/live/ch%d/index.m3u8", number)
		if _, err := tx.Exec(ctx, "INSERT INTO channels (number, name, stream_url, active) VALUES ($1, $2, $3, $4)", number, name, url, number != 5); err != nil {
			return fmt.Errorf("seed channels: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, "INSERT INTO data_seeds (name) VALUES ($1)", seed); err != nil {
		return fmt.Errorf("record seed: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit seed: %w", err)
	}
	return nil
}
