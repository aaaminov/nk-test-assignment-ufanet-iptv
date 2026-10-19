package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"nk-test-assignment-ufanet-iptv/internal/iptv"
)

type PlaybackGetter interface {
	GetPlayback(ctx context.Context, id int64) (iptv.Playback, error)
}

func getPlayback(player PlaybackGetter, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			writeError(w, logger, http.StatusBadRequest, "invalid_id", "channel id must be a positive int64")
			return
		}

		playback, err := player.GetPlayback(r.Context(), id)
		if errors.Is(err, iptv.ErrChannelNotFound) {
			writeError(w, logger, http.StatusNotFound, "channel_not_found", "channel not found")
			return
		}
		if err != nil {
			logger.Error("get playback failed")
			writeError(w, logger, http.StatusInternalServerError, "internal_error", "internal server error")
			return
		}

		response := struct {
			ChannelID int64  `json:"channel_id"`
			URL       string `json:"url"`
		}{ChannelID: playback.ChannelID, URL: playback.URL}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			logger.Error("write playback response failed")
		}
	}
}
