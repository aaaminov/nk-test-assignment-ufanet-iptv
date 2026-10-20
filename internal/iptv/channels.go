package iptv

import (
	"context"
	"fmt"
)

type Channel struct {
	ID     int64
	Number int
	Name   string
}

type ChannelStore interface {
	ListSubscribedChannels(ctx context.Context, userID int64) ([]Channel, error)
}

type Catalog struct {
	channels ChannelStore
}

func NewCatalog(channels ChannelStore) *Catalog {
	return &Catalog{channels: channels}
}

// ListChannels returns the user's subscribed active channels in viewing order.
func (c *Catalog) ListChannels(ctx context.Context, userID int64) ([]Channel, error) {
	channels, err := c.channels.ListSubscribedChannels(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	return channels, nil
}
