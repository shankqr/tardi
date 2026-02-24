package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shanq/tardi/internal/auth"
	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/models"
)

const UserKey contextKey = "user"

func Auth(pool *pgxpool.Pool, devMode bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearerToken(r)
			if token == "" {
				http.Error(w, `{"error":"missing authorization token","code":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			var user *models.User
			var err error

			if devMode && token == "mock-token" {
				// Dev mock user
				user, err = db.UpsertUser(r.Context(), pool, "mock-uid-12345", "demo@tardi.app", nil)
			} else if devMode && auth.Client == nil {
				// Dev mode without Firebase — treat token as UID
				user, err = db.UpsertUser(r.Context(), pool, token, "dev@tardi.app", nil)
			} else if auth.Client != nil {
				// Production: verify Firebase JWT
				decoded, verifyErr := auth.Client.VerifyIDToken(r.Context(), token)
				if verifyErr != nil {
					slog.Warn("auth: invalid firebase token", "error", verifyErr)
					http.Error(w, `{"error":"invalid or expired token","code":"unauthorized"}`, http.StatusUnauthorized)
					return
				}

				email, _ := decoded.Claims["email"].(string)
				name, _ := decoded.Claims["name"].(string)
				var namePtr *string
				if name != "" {
					namePtr = &name
				}

				user, err = db.UpsertUser(r.Context(), pool, decoded.UID, email, namePtr)
			} else {
				http.Error(w, `{"error":"firebase auth not configured","code":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			if err != nil {
				slog.Error("auth: failed to upsert user", "error", err)
				http.Error(w, `{"error":"internal error","code":"internal_error"}`, http.StatusInternalServerError)
				return
			}

			ctx := context.WithValue(r.Context(), UserKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func UserFromContext(ctx context.Context) *models.User {
	if u, ok := ctx.Value(UserKey).(*models.User); ok {
		return u
	}
	return nil
}

func extractBearerToken(r *http.Request) string {
	a := r.Header.Get("Authorization")
	if !strings.HasPrefix(a, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(a, "Bearer ")
}
