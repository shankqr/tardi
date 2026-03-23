package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"crypto/rand"
	"encoding/hex"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/shanq/tardi/internal/api/middleware"
	"github.com/shanq/tardi/internal/crypto"
	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/models"
)

// Google OAuth scopes for full productivity suite access.
var googleScopes = []string{
	"https://www.googleapis.com/auth/calendar",
	"https://www.googleapis.com/auth/gmail.modify",
	"https://www.googleapis.com/auth/drive",
	"https://www.googleapis.com/auth/documents",
	"https://www.googleapis.com/auth/spreadsheets",
	"https://www.googleapis.com/auth/userinfo.email",
}

// oauthStateStore is a simple in-memory store for CSRF state tokens.
// In production with multiple Cloud Run instances, consider using the DB
// or a signed JWT. The 10-minute TTL is short enough that the user will
// almost certainly hit the same instance.
type oauthStateStore struct {
	mu     sync.Mutex
	states map[string]stateEntry
}

type stateEntry struct {
	userID    uuid.UUID
	expiresAt time.Time
}

var stateStore = &oauthStateStore{
	states: make(map[string]stateEntry),
}

func (s *oauthStateStore) Set(state string, userID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Clean up expired entries
	now := time.Now()
	for k, v := range s.states {
		if now.After(v.expiresAt) {
			delete(s.states, k)
		}
	}
	s.states[state] = stateEntry{
		userID:    userID,
		expiresAt: now.Add(10 * time.Minute),
	}
}

func (s *oauthStateStore) Get(state string) (uuid.UUID, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.states[state]
	if !ok || time.Now().After(entry.expiresAt) {
		delete(s.states, state)
		return uuid.UUID{}, false
	}
	delete(s.states, state) // one-time use
	return entry.userID, true
}

func googleOAuthConfig(deps Dependencies) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     deps.Config.GoogleOAuthClientID,
		ClientSecret: deps.Config.GoogleOAuthClientSecret,
		RedirectURL:  deps.Config.APIURL + "/api/oauth/google/callback",
		Scopes:       googleScopes,
		Endpoint:     google.Endpoint,
	}
}

// GoogleOAuthAuthorizeHandler initiates the Google OAuth flow.
// GET /api/oauth/google/authorize
func GoogleOAuthAuthorizeHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
			return
		}

		if deps.Config.GoogleOAuthClientID == "" {
			WriteError(w, http.StatusServiceUnavailable, "not_configured", "Google OAuth is not configured")
			return
		}

		// Generate CSRF state token
		stateBytes := make([]byte, 32)
		if _, err := rand.Read(stateBytes); err != nil {
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to generate state")
			return
		}
		state := hex.EncodeToString(stateBytes)
		stateStore.Set(state, user.ID)

		cfg := googleOAuthConfig(deps)
		url := cfg.AuthCodeURL(state,
			oauth2.AccessTypeOffline,
			oauth2.SetAuthURLParam("prompt", "consent"),
		)

		WriteJSON(w, http.StatusOK, map[string]string{
			"redirect_url": url,
		})
	}
}

// GoogleOAuthCallbackHandler handles the OAuth callback from Google.
// GET /api/oauth/google/callback
func GoogleOAuthCallbackHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check for error from Google
		if errParam := r.URL.Query().Get("error"); errParam != "" {
			renderOAuthResult(w, false, "", "Google authorization was denied: "+errParam)
			return
		}

		state := r.URL.Query().Get("state")
		code := r.URL.Query().Get("code")

		if state == "" || code == "" {
			renderOAuthResult(w, false, "", "Missing state or code parameter")
			return
		}

		// Validate state
		userID, ok := stateStore.Get(state)
		if !ok {
			renderOAuthResult(w, false, "", "Invalid or expired state token. Please try again.")
			return
		}

		cfg := googleOAuthConfig(deps)

		// Exchange code for tokens
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()

		token, err := cfg.Exchange(ctx, code)
		if err != nil {
			slog.Error("google oauth: token exchange failed", "error", err)
			renderOAuthResult(w, false, "", "Failed to exchange authorization code")
			return
		}

		if token.RefreshToken == "" {
			slog.Warn("google oauth: no refresh token received", "user_id", userID)
			renderOAuthResult(w, false, "", "No refresh token received. Please revoke access at myaccount.google.com and try again.")
			return
		}

		// Fetch Google email
		email, err := fetchGoogleEmail(ctx, cfg, token)
		if err != nil {
			slog.Error("google oauth: failed to fetch email", "error", err)
			renderOAuthResult(w, false, "", "Failed to fetch Google account info")
			return
		}

		// Encrypt tokens
		encKey, err := crypto.ParseKey(deps.Config.TokenEncryptionKey)
		if err != nil {
			slog.Error("google oauth: invalid encryption key", "error", err)
			renderOAuthResult(w, false, "", "Server configuration error")
			return
		}

		accessEnc, err := crypto.Encrypt([]byte(token.AccessToken), encKey)
		if err != nil {
			slog.Error("google oauth: encrypt access token", "error", err)
			renderOAuthResult(w, false, "", "Failed to secure tokens")
			return
		}

		refreshEnc, err := crypto.Encrypt([]byte(token.RefreshToken), encKey)
		if err != nil {
			slog.Error("google oauth: encrypt refresh token", "error", err)
			renderOAuthResult(w, false, "", "Failed to secure tokens")
			return
		}

		// Store in DB
		oauthToken := &models.GoogleOAuthToken{
			ID:              uuid.New(),
			UserID:          userID,
			GoogleEmail:     email,
			AccessTokenEnc:  accessEnc,
			RefreshTokenEnc: refreshEnc,
			TokenExpiry:     token.Expiry,
			Scopes:          googleScopes,
		}

		if err := db.UpsertGoogleOAuthToken(ctx, deps.Pool, oauthToken); err != nil {
			slog.Error("google oauth: save token", "error", err, "user_id", userID)
			renderOAuthResult(w, false, "", "Failed to save authorization")
			return
		}

		// Bump agent config version to trigger sync
		if err := bumpConfigVersion(ctx, deps, userID); err != nil {
			slog.Warn("google oauth: bump config version", "error", err, "user_id", userID)
			// Non-fatal — tokens are saved, sync will catch up on next heartbeat
		}

		slog.Info("google oauth: connected", "user_id", userID, "email", email)
		renderOAuthResult(w, true, email, "")
	}
}

// GoogleOAuthStatusHandler returns the current Google OAuth connection status.
// GET /api/oauth/google/status
func GoogleOAuthStatusHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
			return
		}

		token, err := db.GetGoogleOAuthTokenByUserID(r.Context(), deps.Pool, user.ID)
		if err != nil {
			WriteJSON(w, http.StatusOK, map[string]any{
				"connected": false,
			})
			return
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"connected": true,
			"email":     token.GoogleEmail,
			"scopes":    token.Scopes,
		})
	}
}

// GoogleOAuthDisconnectHandler revokes Google access and removes tokens.
// POST /api/oauth/google/disconnect
func GoogleOAuthDisconnectHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
			return
		}

		token, err := db.GetGoogleOAuthTokenByUserID(r.Context(), deps.Pool, user.ID)
		if err != nil {
			WriteJSON(w, http.StatusOK, map[string]any{"disconnected": true})
			return
		}

		// Revoke token with Google (best effort)
		encKey, _ := crypto.ParseKey(deps.Config.TokenEncryptionKey)
		if encKey != nil {
			accessToken, decErr := crypto.Decrypt(token.AccessTokenEnc, encKey)
			if decErr == nil {
				revokeCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
				defer cancel()
				revokeGoogleToken(revokeCtx, string(accessToken))
			}
		}

		// Mark revoked in DB
		if err := db.RevokeGoogleOAuthToken(r.Context(), deps.Pool, user.ID); err != nil {
			slog.Error("google oauth disconnect: revoke", "error", err, "user_id", user.ID)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to disconnect")
			return
		}

		// Bump agent config version to trigger sync (removes creds from VPS)
		if err := bumpConfigVersion(r.Context(), deps, user.ID); err != nil {
			slog.Warn("google oauth disconnect: bump config version", "error", err)
		}

		slog.Info("google oauth: disconnected", "user_id", user.ID, "email", token.GoogleEmail)
		WriteJSON(w, http.StatusOK, map[string]any{"disconnected": true})
	}
}

// fetchGoogleEmail calls the Google userinfo endpoint to get the user's email.
func fetchGoogleEmail(ctx context.Context, cfg *oauth2.Config, token *oauth2.Token) (string, error) {
	client := cfg.Client(ctx, token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		return "", fmt.Errorf("fetch userinfo: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read userinfo body: %w", err)
	}

	var info struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return "", fmt.Errorf("parse userinfo: %w", err)
	}
	if info.Email == "" {
		return "", fmt.Errorf("no email in userinfo response")
	}
	return info.Email, nil
}

// revokeGoogleToken calls Google's token revocation endpoint.
func revokeGoogleToken(ctx context.Context, token string) {
	req, err := http.NewRequestWithContext(ctx, "POST", "https://oauth2.googleapis.com/revoke?token="+token, nil)
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn("google oauth: revocation request failed", "error", err)
		return
	}
	resp.Body.Close()
}

// bumpConfigVersion bumps the agent config version for all instances owned by a user.
// This triggers the heartbeat sync mechanism to push updated credentials to the VPS.
func bumpConfigVersion(ctx context.Context, deps Dependencies, userID uuid.UUID) error {
	instances, err := db.GetInstancesByUserID(ctx, deps.Pool, userID)
	if err != nil {
		return fmt.Errorf("get instances: %w", err)
	}

	for _, inst := range instances {
		existing, _ := db.GetAgentConfigByInstanceID(ctx, deps.Pool, inst.ID)
		config := make(map[string]any)
		if existing != nil {
			for k, v := range existing.Config {
				config[k] = v
			}
		}

		ac := &models.AgentConfig{
			ID:            uuid.New(),
			VpsInstanceID: inst.ID,
			Config:        config,
			Version:       1,
		}
		if err := db.CreateAgentConfig(ctx, deps.Pool, ac); err != nil {
			return fmt.Errorf("bump config for instance %s: %w", inst.ID, err)
		}
	}
	return nil
}

// renderOAuthResult renders the callback HTML page that communicates the result
// back to the opener window via postMessage.
func renderOAuthResult(w http.ResponseWriter, success bool, email string, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	successStr := "false"
	if success {
		successStr = "true"
	}

	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><title>Google Authorization</title></head>
<body>
<script>
(function() {
    var msg = {
        type: 'google-oauth-result',
        success: %s,
        email: %q,
        error: %q
    };
    if (window.opener) {
        window.opener.postMessage(msg, '*');
    }
    window.close();
    // Fallback if window.close() doesn't work
    document.body.innerHTML = msg.success
        ? '<p>Google account connected! You can close this window.</p>'
        : '<p>Error: ' + msg.error + '</p>';
})();
</script>
</body>
</html>`, successStr, email, errMsg)

	w.Write([]byte(html))
}
