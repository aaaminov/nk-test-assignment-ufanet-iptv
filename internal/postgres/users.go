package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"nk-test-assignment-ufanet-iptv/internal/iptv"
)

type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository uses the application's pool without taking ownership of its Close.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// LookupUser includes inactive users so the caller can decide whether to allow access.
// Errors may contain database details and must not be logged verbatim.
func (r *Repository) LookupUser(ctx context.Context, login string) (iptv.User, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var user iptv.User
	err := r.pool.QueryRow(ctx, `SELECT id, password_hash, active FROM users WHERE login = $1`, login).
		Scan(&user.ID, &user.PasswordHash, &user.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return iptv.User{}, iptv.ErrUserNotFound
	}
	if err != nil {
		return iptv.User{}, fmt.Errorf("lookup user: %w", err)
	}
	return user, nil
}
