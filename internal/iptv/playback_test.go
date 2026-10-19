package iptv_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"nk-test-assignment-ufanet-iptv/internal/iptv"
)

type playbackStoreFunc func(context.Context, int64) (iptv.Playback, error)

func (f playbackStoreFunc) LookupActivePlayback(ctx context.Context, id int64) (iptv.Playback, error) {
	return f(ctx, id)
}

func TestPlayerGetPlayback(t *testing.T) {
	playback := iptv.Playback{ChannelID: 42, URL: "https://stream.example.com/live/custom/index.m3u8"}
	failure := errors.New("store unavailable")
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"active channel", nil},
		{"unavailable channel", iptv.ErrChannelNotFound},
		{"store failure", failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := playbackStoreFunc(func(ctx context.Context, id int64) (iptv.Playback, error) {
				if ctx != t.Context() || id != playback.ChannelID {
					t.Fatal("player must forward the caller context and channel ID")
				}
				if tc.err != nil {
					return playback, fmt.Errorf("lookup: %w", tc.err)
				}
				return playback, nil
			})
			got, err := iptv.NewPlayer(store).GetPlayback(t.Context(), playback.ChannelID)
			if tc.err != nil {
				if !errors.Is(err, tc.err) || got != (iptv.Playback{}) {
					t.Fatal("player must preserve the cause and discard results on error")
				}
			} else if err != nil || got != playback {
				t.Fatalf("playback = %#v, error = %v; want %#v", got, err, playback)
			}
		})
	}
}
