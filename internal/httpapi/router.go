package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"nk-test-assignment-ufanet-iptv/internal/iptv"
)

type ChannelLister interface {
	ListChannels(ctx context.Context) ([]iptv.Channel, error)
}

func NewRouter(users UserLookup, channels ChannelLister, playback PlaybackGetter, logger *slog.Logger) http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/channels", listChannels(channels, logger))
	api.HandleFunc("GET /api/v1/channels/{id}/playback", getPlayback(playback, logger))

	mux := http.NewServeMux()
	mux.Handle("/api/v1/", BasicAuth(users, logger, api))
	return mux
}

type channelResponse struct {
	ID     int64  `json:"id"`
	Number int    `json:"number"`
	Name   string `json:"name"`
}

func listChannels(catalog ChannelLister, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		channels, err := catalog.ListChannels(r.Context())
		if err != nil {
			logger.Error("list channels failed")
			writeError(w, logger, http.StatusInternalServerError, "internal_error", "internal server error")
			return
		}

		response := struct {
			Channels []channelResponse `json:"channels"`
		}{Channels: make([]channelResponse, 0, len(channels))}
		for _, channel := range channels {
			response.Channels = append(response.Channels, channelResponse{
				ID: channel.ID, Number: channel.Number, Name: channel.Name,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			logger.Error("write channels response failed")
		}
	}
}
