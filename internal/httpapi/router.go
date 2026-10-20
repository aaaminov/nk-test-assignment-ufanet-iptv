package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"nk-test-assignment-ufanet-iptv/internal/iptv"
)

type ChannelLister interface {
	ListChannels(ctx context.Context, userID int64) ([]iptv.Channel, error)
}

func NewRouter(users UserLookup, channels ChannelLister, playback PlaybackGetter, logger *slog.Logger) http.Handler {
	notFound := func(w http.ResponseWriter, r *http.Request) {
		writeError(w, logger, http.StatusNotFound, "not_found", "resource not found")
	}
	methodNotAllowed := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, logger, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}

	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/channels", listChannels(channels, logger))
	api.HandleFunc("/api/v1/channels", methodNotAllowed)
	api.HandleFunc("GET /api/v1/channels/{id}/playback", getPlayback(playback, logger))
	api.HandleFunc("/api/v1/channels/{id}/playback", methodNotAllowed)
	api.HandleFunc("/", notFound)

	protected := BasicAuth(users, logger, api)
	return accessLog(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Authenticate API requests before ServeMux can canonicalize their paths.
		if r.URL.Path == "/api/v1" || strings.HasPrefix(r.URL.Path, "/api/v1/") {
			protected.ServeHTTP(w, r)
			return
		}
		notFound(w, r)
	}))
}

type channelResponse struct {
	ID     int64  `json:"id"`
	Number int    `json:"number"`
	Name   string `json:"name"`
}

func listChannels(catalog ChannelLister, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(userIDKey{}).(int64)
		channels, err := catalog.ListChannels(r.Context(), userID)
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
