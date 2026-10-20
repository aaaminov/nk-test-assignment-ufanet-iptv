package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"nk-test-assignment-ufanet-iptv/internal/iptv"
)

// ListSubscribedChannels returns active channels with an enabled, unexpired subscription.
// Expiration is exclusive and checked against the database statement time.
// Errors may contain database details and must not be logged verbatim.
func (r *Repository) ListSubscribedChannels(ctx context.Context, userID int64) ([]iptv.Channel, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	rows, err := r.pool.Query(ctx, `SELECT c.id, c.number, c.name
		FROM channels c JOIN subscriptions s ON s.channel_id = c.id
		WHERE c.active AND s.user_id = $1 AND s.active
			AND (s.expires_at IS NULL OR s.expires_at > statement_timestamp())
		ORDER BY c.number, c.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("query subscribed channels: %w", err)
	}
	defer rows.Close()

	var channels []iptv.Channel
	for rows.Next() {
		var channel iptv.Channel
		if err := rows.Scan(&channel.ID, &channel.Number, &channel.Name); err != nil {
			return nil, fmt.Errorf("scan subscribed channel: %w", err)
		}
		channels = append(channels, channel)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read subscribed channels: %w", err)
	}
	return channels, nil
}

// LookupPlaybackAccess reads an active channel and its subscription permission in one query.
// A missing or disabled channel returns ErrChannelNotFound; no subscription returns Allowed=false.
// The caller authenticates the user. Errors must not be logged verbatim.
func (r *Repository) LookupPlaybackAccess(ctx context.Context, userID, channelID int64) (iptv.PlaybackAccess, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var access iptv.PlaybackAccess
	err := r.pool.QueryRow(ctx, `SELECT c.id, c.stream_url, EXISTS (
		SELECT 1 FROM subscriptions s
		WHERE s.user_id = $1 AND s.channel_id = c.id AND s.active
			AND (s.expires_at IS NULL OR s.expires_at > statement_timestamp())
		) FROM channels c WHERE c.id = $2 AND c.active`, userID, channelID).
		Scan(&access.Playback.ChannelID, &access.Playback.URL, &access.Allowed)
	if errors.Is(err, pgx.ErrNoRows) {
		return iptv.PlaybackAccess{}, iptv.ErrChannelNotFound
	}
	if err != nil {
		return iptv.PlaybackAccess{}, fmt.Errorf("lookup playback access: %w", err)
	}
	return access, nil
}
