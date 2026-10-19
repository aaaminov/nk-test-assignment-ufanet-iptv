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
	ListActiveChannels(ctx context.Context) ([]Channel, error)
}

type Catalog struct {
	channels ChannelStore
}

func NewCatalog(channels ChannelStore) *Catalog {
	return &Catalog{channels: channels}
}

// ListChannels returns the active channels in viewing order.
func (c *Catalog) ListChannels(ctx context.Context) ([]Channel, error) {
	channels, err := c.channels.ListActiveChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	return channels, nil
}
