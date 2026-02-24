package auth

import (
	"context"
	"fmt"
	"log/slog"

	firebase "firebase.google.com/go/v4"
	fbauth "firebase.google.com/go/v4/auth"
)

// Client wraps the Firebase Auth client for JWT verification.
var Client *fbauth.Client

// InitFirebase initializes the Firebase Admin SDK for JWT verification.
// In dev mode or when no project ID is set, Client remains nil and
// the auth middleware falls back to dev-mode behavior.
func InitFirebase(projectID string, devMode bool) error {
	if devMode || projectID == "" {
		slog.Info("firebase: skipped (dev mode or no project ID)")
		return nil
	}

	ctx := context.Background()
	app, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: projectID})
	if err != nil {
		return fmt.Errorf("firebase: failed to initialize app: %w", err)
	}

	client, err := app.Auth(ctx)
	if err != nil {
		return fmt.Errorf("firebase: failed to get auth client: %w", err)
	}

	Client = client
	slog.Info("firebase: initialized", "project_id", projectID)
	return nil
}
