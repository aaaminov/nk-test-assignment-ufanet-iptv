package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"nk-test-assignment-ufanet-iptv/internal/auth"
	"nk-test-assignment-ufanet-iptv/internal/iptv"
)

type UserLookup interface {
	LookupUser(ctx context.Context, login string) (iptv.User, error)
}

type userIDKey struct{}

// BasicAuth allows only active users with a matching password to reach next.
func BasicAuth(users UserLookup, logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		login, password, ok := r.BasicAuth()
		if ok {
			user, err := users.LookupUser(r.Context(), login)
			if err != nil && !errors.Is(err, iptv.ErrUserNotFound) {
				// Driver errors can contain credentials or SQL; log only the operation.
				logger.Error("lookup user failed")
				writeError(w, logger, http.StatusInternalServerError, "internal_error", "internal server error")
				return
			}
			if err == nil && user.Active && auth.CheckPassword(user.PasswordHash, password) {
				ctx := context.WithValue(r.Context(), userIDKey{}, user.ID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		w.Header().Set("WWW-Authenticate", `Basic realm="iptv"`)
		writeError(w, logger, http.StatusUnauthorized, "unauthorized", "authentication required")
	})
}
