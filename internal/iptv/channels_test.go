package iptv_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"nk-test-assignment-ufanet-iptv/internal/iptv"
)

type channelStoreFunc func(context.Context) ([]iptv.Channel, error)

func (f channelStoreFunc) ListActiveChannels(ctx context.Context) ([]iptv.Channel, error) {
	return f(ctx)
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
			store := channelStoreFunc(func(ctx context.Context) ([]iptv.Channel, error) {
				if ctx != t.Context() {
					t.Fatal("catalog must forward the caller context")
				}
				return tc.channels, tc.err
			})
			got, err := iptv.NewCatalog(store).ListChannels(t.Context())
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
