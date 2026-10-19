package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"nk-test-assignment-ufanet-iptv/internal/iptv"
)

// LookupActivePlayback treats missing and disabled channels alike.
// Errors may contain database details and must not be logged verbatim.
func (r *Repository) LookupActivePlayback(ctx context.Context, id int64) (iptv.Playback, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var playback iptv.Playback
	err := r.pool.QueryRow(ctx, `SELECT id, stream_url FROM channels WHERE id = $1 AND active`, id).
		Scan(&playback.ChannelID, &playback.URL)
	if errors.Is(err, pgx.ErrNoRows) {
		return iptv.Playback{}, iptv.ErrChannelNotFound
	}
	if err != nil {
		return iptv.Playback{}, fmt.Errorf("lookup active playback: %w", err)
	}
	return playback, nil
}
