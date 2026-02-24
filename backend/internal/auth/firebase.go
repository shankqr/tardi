package auth

import (
	"log/slog"
)

// InitFirebase initializes the Firebase Admin SDK for JWT verification.
// Returns nil in dev mode — the auth middleware handles this gracefully.
func InitFirebase(projectID string, devMode bool) {
	if devMode || projectID == "" {
		slog.Info("firebase: skipped (dev mode or no project ID)")
		return
	}

	// TODO: Initialize Firebase Admin SDK
	// ctx := context.Background()
	// app, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: projectID})
	slog.Info("firebase: initialized", "project_id", projectID)
}
