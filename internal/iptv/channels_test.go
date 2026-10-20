package iptv_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"nk-test-assignment-ufanet-iptv/internal/iptv"
)

type channelStoreFunc func(context.Context, int64) ([]iptv.Channel, error)

func (f channelStoreFunc) ListSubscribedChannels(ctx context.Context, userID int64) ([]iptv.Channel, error) {
	return f(ctx, userID)
}

func TestCatalogListChannels(t *testing.T) {
	failure := errors.New("store unavailable")
	ordered := []iptv.Channel{
		{ID: 3, Number: 1, Name: "Первый"},
		{ID: 1, Number: 2, Name: "Россия 1"},
	}
	for _, tc := range []struct {
		name     string
		channels []iptv.Channel
		err      error
	}{
		{"catalog", ordered, nil},
		{"empty", nil, nil},
		{"failure after partial result", ordered, failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := channelStoreFunc(func(ctx context.Context, userID int64) ([]iptv.Channel, error) {
				if ctx != t.Context() || userID != 7 {
					t.Fatal("catalog must forward the caller context and user ID")
				}
				return tc.channels, tc.err
			})
			got, err := iptv.NewCatalog(store).ListChannels(t.Context(), 7)
			if tc.err != nil {
				if !errors.Is(err, tc.err) || got != nil {
					t.Fatal("catalog must preserve the error and discard partial results")
				}
			} else if err != nil || !reflect.DeepEqual(got, tc.channels) {
				t.Fatalf("channels = %#v, error = %v; want %#v", got, err, tc.channels)
			}
		})
	}
}
