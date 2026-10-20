package iptv_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"nk-test-assignment-ufanet-iptv/internal/iptv"
)

type playbackStoreFunc func(context.Context, int64, int64) (iptv.PlaybackAccess, error)

func (f playbackStoreFunc) LookupPlaybackAccess(ctx context.Context, userID, id int64) (iptv.PlaybackAccess, error) {
	return f(ctx, userID, id)
}

func TestPlayerGetPlayback(t *testing.T) {
	playback := iptv.Playback{ChannelID: 42, URL: "https://stream.example.com/live/custom/index.m3u8"}
	failure := errors.New("store unavailable")
	for _, tc := range []struct {
		name    string
		allowed bool
		err     error
		wantErr error
	}{
		{"subscribed channel", true, nil, nil},
		{"no permission", false, nil, iptv.ErrSubscriptionRequired},
		{"unavailable channel", false, iptv.ErrChannelNotFound, iptv.ErrChannelNotFound},
		{"store failure", false, failure, failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := playbackStoreFunc(func(ctx context.Context, userID, id int64) (iptv.PlaybackAccess, error) {
				if ctx != t.Context() || userID != 7 || id != playback.ChannelID {
					t.Fatal("player must forward the caller context, user ID and channel ID")
				}
				access := iptv.PlaybackAccess{Playback: playback, Allowed: tc.allowed}
				if tc.err != nil {
					return access, fmt.Errorf("lookup: %w", tc.err)
				}
				return access, nil
			})
			got, err := iptv.NewPlayer(store).GetPlayback(t.Context(), 7, playback.ChannelID)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) || got != (iptv.Playback{}) {
					t.Fatal("player must return the expected error without exposing the playback")
				}
			} else if err != nil || got != playback {
				t.Fatalf("playback = %#v, error = %v; want %#v", got, err, playback)
			}
		})
	}
}
