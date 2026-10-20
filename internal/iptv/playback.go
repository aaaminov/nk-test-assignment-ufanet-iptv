package iptv

import (
	"context"
	"errors"
	"fmt"
)

var ErrChannelNotFound = errors.New("channel not found")
var ErrSubscriptionRequired = errors.New("active subscription required")

type Playback struct {
	ChannelID int64
	URL       string
}

// PlaybackAccess distinguishes a found channel from permission to watch it.
type PlaybackAccess struct {
	Playback Playback
	Allowed  bool
}

type PlaybackStore interface {
	LookupPlaybackAccess(ctx context.Context, userID, channelID int64) (PlaybackAccess, error)
}

type Player struct {
	channels PlaybackStore
}

func NewPlayer(channels PlaybackStore) *Player {
	return &Player{channels: channels}
}

// GetPlayback returns an active channel's HLS URL only with a valid subscription.
func (p *Player) GetPlayback(ctx context.Context, userID, id int64) (Playback, error) {
	access, err := p.channels.LookupPlaybackAccess(ctx, userID, id)
	if err != nil {
		return Playback{}, fmt.Errorf("get playback: %w", err)
	}
	if !access.Allowed {
		return Playback{}, ErrSubscriptionRequired
	}
	return access.Playback, nil
}
