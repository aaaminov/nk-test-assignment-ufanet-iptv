package iptv

import (
	"context"
	"errors"
	"fmt"
)

var ErrChannelNotFound = errors.New("channel not found")

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
	LookupActivePlayback(ctx context.Context, id int64) (Playback, error)
}

type Player struct {
	channels PlaybackStore
}

func NewPlayer(channels PlaybackStore) *Player {
	return &Player{channels: channels}
}

// GetPlayback returns the stored HLS URL of an active channel.
func (p *Player) GetPlayback(ctx context.Context, id int64) (Playback, error) {
	playback, err := p.channels.LookupActivePlayback(ctx, id)
	if err != nil {
		return Playback{}, fmt.Errorf("get playback: %w", err)
	}
	return playback, nil
}
