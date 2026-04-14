package scripts

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"os"
	"sync"
)

// dashboardShimDefaultPath is the location where the Dockerfile drops the
// compiled tardi-dashboard-shim binary. Override with DASHBOARD_SHIM_PATH for
// local development (defaults to backend/bin/tardi-dashboard-shim relative to
// the cwd, which matches `make build` output).
const dashboardShimDefaultPath = "bin/tardi-dashboard-shim"

var (
	dashboardShimOnce sync.Once
	dashboardShimBin  []byte
	dashboardShimSHA  string
)

// LoadDashboardShim reads the dashboard-shim binary from disk into memory once
// and computes its sha256. Subsequent calls are no-ops. Safe to call from any
// goroutine. If the binary is missing, the byte slice stays nil and the
// handler returns 503 — this lets the backend boot without the binary in
// non-Docker contexts (e.g. unit tests).
func LoadDashboardShim() {
	dashboardShimOnce.Do(func() {
		path := os.Getenv("DASHBOARD_SHIM_PATH")
		if path == "" {
			path = dashboardShimDefaultPath
		}
		data, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("dashboard-shim binary not loaded", "path", path, "error", err)
			return
		}
		sum := sha256.Sum256(data)
		dashboardShimBin = data
		dashboardShimSHA = hex.EncodeToString(sum[:])
		slog.Info("dashboard-shim binary loaded", "path", path, "size", len(data), "sha256", dashboardShimSHA)
	})
}

// DashboardShimBinary returns the loaded shim binary bytes. Nil if not loaded.
func DashboardShimBinary() []byte {
	LoadDashboardShim()
	return dashboardShimBin
}

// DashboardShimSHA256 returns the hex-encoded sha256 of the loaded binary.
// Empty string if not loaded.
func DashboardShimSHA256() string {
	LoadDashboardShim()
	return dashboardShimSHA
}
