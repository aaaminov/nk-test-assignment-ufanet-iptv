package httpapi

import (
	"log/slog"
	"net/http"
	"time"
)

func accessLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		response := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(response, r)
		status := response.status
		if status == 0 {
			status = http.StatusOK
		}
		logger.InfoContext(r.Context(), "HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"duration_ms", float64(time.Since(started))/float64(time.Millisecond),
		)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.ResponseWriter.WriteHeader(status)
	// Informational responses do not determine the final response status.
	if w.status == 0 && (status >= 200 || status == http.StatusSwitchingProtocols) {
		w.status = status
	}
}

func (w *statusWriter) Write(body []byte) (int, error) {
	n, err := w.ResponseWriter.Write(body)
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return n, err
}

func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
