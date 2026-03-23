package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/shanq/tardi/internal/config"
	"github.com/shanq/tardi/internal/crypto"
	"github.com/shanq/tardi/internal/db"
)

// TokenRefresher periodically refreshes Google OAuth tokens before they expire.
type TokenRefresher struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
	cfg    *config.Config
}

func NewTokenRefresher(pool *pgxpool.Pool, logger *slog.Logger, cfg *config.Config) *TokenRefresher {
	return &TokenRefresher{pool: pool, logger: logger, cfg: cfg}
}

// Start runs the token refresh loop. Call in a goroutine.
func (tr *TokenRefresher) Start(ctx context.Context) {
	if tr.cfg.GoogleOAuthClientID == "" || tr.cfg.TokenEncryptionKey == "" {
		tr.logger.Info("token refresher: skipping (Google OAuth not configured)")
		return
	}

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tr.refreshExpiring(ctx)
		}
	}
}

func (tr *TokenRefresher) refreshExpiring(ctx context.Context) {
	tokens, err := db.GetExpiringSoonGoogleTokens(ctx, tr.pool, 10*time.Minute)
	if err != nil {
		tr.logger.Error("token refresher: query expiring tokens", "error", err)
		return
	}

	if len(tokens) == 0 {
		return
	}

	encKey, err := crypto.ParseKey(tr.cfg.TokenEncryptionKey)
	if err != nil {
		tr.logger.Error("token refresher: invalid encryption key", "error", err)
		return
	}

	oauthCfg := &oauth2.Config{
		ClientID:     tr.cfg.GoogleOAuthClientID,
		ClientSecret: tr.cfg.GoogleOAuthClientSecret,
		Endpoint:     google.Endpoint,
	}

	for _, t := range tokens {
		refreshToken, err := crypto.Decrypt(t.RefreshTokenEnc, encKey)
		if err != nil {
			tr.logger.Error("token refresher: decrypt refresh token", "error", err, "token_id", t.ID)
			continue
		}

		oldToken := &oauth2.Token{
			RefreshToken: string(refreshToken),
			Expiry:       t.TokenExpiry,
		}

		src := oauthCfg.TokenSource(ctx, oldToken)
		newToken, err := src.Token()
		if err != nil {
			tr.logger.Warn("token refresher: refresh failed (user may have revoked access)",
				"error", err, "token_id", t.ID, "email", t.GoogleEmail)
			// Mark as revoked if refresh fails
			if revokeErr := db.RevokeGoogleOAuthToken(ctx, tr.pool, t.UserID); revokeErr != nil {
				tr.logger.Error("token refresher: revoke failed", "error", revokeErr, "token_id", t.ID)
			}
			continue
		}

		newAccessEnc, err := crypto.Encrypt([]byte(newToken.AccessToken), encKey)
		if err != nil {
			tr.logger.Error("token refresher: encrypt access token", "error", err, "token_id", t.ID)
			continue
		}

		// Google may rotate the refresh token
		newRefreshEnc := t.RefreshTokenEnc
		if newToken.RefreshToken != "" && newToken.RefreshToken != string(refreshToken) {
			newRefreshEnc, err = crypto.Encrypt([]byte(newToken.RefreshToken), encKey)
			if err != nil {
				tr.logger.Error("token refresher: encrypt new refresh token", "error", err, "token_id", t.ID)
				continue
			}
		}

		if err := db.UpdateGoogleOAuthTokenFields(ctx, tr.pool, t.ID, newAccessEnc, newRefreshEnc, newToken.Expiry); err != nil {
			tr.logger.Error("token refresher: update token", "error", err, "token_id", t.ID)
			continue
		}

		tr.logger.Info("token refresher: refreshed", "email", t.GoogleEmail, "new_expiry", newToken.Expiry)
	}
}
