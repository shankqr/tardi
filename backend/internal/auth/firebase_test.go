package auth

import (
	"testing"
)

func TestInitFirebase_EmptyProjectID(t *testing.T) {
	// Reset Client to ensure clean state
	Client = nil

	err := InitFirebase("", false)
	if err != nil {
		t.Fatalf("InitFirebase with empty project ID should not error, got: %v", err)
	}
	if Client != nil {
		t.Error("Client should remain nil when project ID is empty")
	}
}

func TestInitFirebase_EmptyProjectID_DevMode(t *testing.T) {
	Client = nil

	err := InitFirebase("", true)
	if err != nil {
		t.Fatalf("InitFirebase with empty project ID in dev mode should not error, got: %v", err)
	}
	if Client != nil {
		t.Error("Client should remain nil when project ID is empty")
	}
}
