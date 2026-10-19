package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type errorResponse struct {
	Error errorDetails `json:"error"`
}

type errorDetails struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, logger *slog.Logger, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(errorResponse{Error: errorDetails{Code: code, Message: message}}); err != nil {
		// The status is already sent; another HTTP response cannot be written.
		logger.Error("write error response failed")
	}
}
