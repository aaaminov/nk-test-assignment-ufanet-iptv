package postgres

import (
	"context"
	"fmt"
	"time"

	"nk-test-assignment-ufanet-iptv/internal/iptv"
)

// ListActiveChannels returns only catalog fields, ordered by number and then ID.
// Errors may contain database details and must not be logged verbatim.
func (r *Repository) ListActiveChannels(ctx context.Context) ([]iptv.Channel, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	rows, err := r.pool.Query(ctx, `SELECT id, number, name FROM channels WHERE active ORDER BY number, id`)
	if err != nil {
		return nil, fmt.Errorf("query active channels: %w", err)
	}
	defer rows.Close()

	var channels []iptv.Channel
	for rows.Next() {
		var channel iptv.Channel
		if err := rows.Scan(&channel.ID, &channel.Number, &channel.Name); err != nil {
			return nil, fmt.Errorf("scan active channel: %w", err)
		}
		channels = append(channels, channel)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read active channels: %w", err)
	}
	return channels, nil
}
