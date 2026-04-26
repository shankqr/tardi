// Synthetic monitor: runs periodic health checks against prod and
// opens/closes a GitHub "outage" issue on failure/recovery. Designed to
// run as a Cloud Run Job invoked by Cloud Scheduler.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

type checkResult struct {
	Name    string
	OK      bool
	Status  int
	Latency time.Duration
	Detail  string
}

type config struct {
	apiURL      string
	frontendURL string
	ghToken     string
	ghRepo      string
	sslHost     string
	sslWarnDays int
}

func loadConfig() (config, error) {
	cfg := config{
		apiURL:      getenv("API_URL", "https://api.tardi.ai"),
		frontendURL: getenv("FRONTEND_URL", "https://app.tardi.ai"),
		ghToken:     os.Getenv("GITHUB_TOKEN"),
		ghRepo:      getenv("GITHUB_REPO", "shankqr/tardi"),
		sslHost:     getenv("SSL_HOST", "app.tardi.ai"),
		sslWarnDays: 14,
	}
	if cfg.ghToken == "" {
		return cfg, fmt.Errorf("GITHUB_TOKEN is required")
	}
	if !strings.Contains(cfg.ghRepo, "/") {
		return cfg, fmt.Errorf("GITHUB_REPO must be owner/repo")
	}
	return cfg, nil
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := loadConfig()
	if err != nil {
		slog.Error("bad config", "error", err)
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	results := runChecks(ctx, cfg)

	failed := 0
	for _, r := range results {
		logAttrs := []any{"check", r.Name, "ok", r.OK, "latency_ms", r.Latency.Milliseconds()}
		if r.Status != 0 {
			logAttrs = append(logAttrs, "status", r.Status)
		}
		if r.Detail != "" {
			logAttrs = append(logAttrs, "detail", r.Detail)
		}
		if r.OK {
			slog.Info("check passed", logAttrs...)
		} else {
			slog.Error("check failed", logAttrs...)
			failed++
		}
	}

	if failed > 0 {
		if err := reportFailure(ctx, cfg, results); err != nil {
			slog.Error("failed to report failure to github", "error", err)
		}
		os.Exit(1)
	}

	if err := resolveOutage(ctx, cfg); err != nil {
		slog.Error("failed to resolve outage issue", "error", err)
	}
}

func runChecks(ctx context.Context, cfg config) []checkResult {
	httpClient := &http.Client{Timeout: 10 * time.Second}

	checks := []struct {
		name       string
		url        string
		timeout    time.Duration
		substring  string
	}{
		{"API /readyz", cfg.apiURL + "/readyz", 5 * time.Second, ""},
		{"API /api/models", cfg.apiURL + "/api/models", 5 * time.Second, ""},
		{"Frontend /", cfg.frontendURL, 10 * time.Second, "tardi"},
		{"Frontend /login", cfg.frontendURL + "/login", 10 * time.Second, ""},
	}

	results := make([]checkResult, 0, len(checks)+1)
	for _, c := range checks {
		cctx, cancel := context.WithTimeout(ctx, c.timeout)
		results = append(results, httpCheck(cctx, httpClient, c.name, c.url, c.substring))
		cancel()
	}

	results = append(results, sslCheck(cfg.sslHost, cfg.sslWarnDays))
	return results
}

func httpCheck(ctx context.Context, client *http.Client, name, urlStr, substring string) checkResult {
	r := checkResult{Name: name}
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		r.Detail = err.Error()
		r.Latency = time.Since(start)
		return r
	}
	resp, err := client.Do(req)
	r.Latency = time.Since(start)
	if err != nil {
		r.Detail = err.Error()
		return r
	}
	defer resp.Body.Close()
	r.Status = resp.StatusCode
	if resp.StatusCode != http.StatusOK {
		r.Detail = fmt.Sprintf("HTTP %d", resp.StatusCode)
		return r
	}
	if substring != "" {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		if !strings.Contains(strings.ToLower(string(body)), strings.ToLower(substring)) {
			r.Detail = "200 but missing expected substring: " + substring
			return r
		}
	}
	r.OK = true
	return r
}

func sslCheck(host string, warnDays int) checkResult {
	r := checkResult{Name: "SSL Certificate"}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", host+":443", &tls.Config{ServerName: host})
	if err != nil {
		r.Detail = "could not establish TLS connection: " + err.Error()
		return r
	}
	defer conn.Close()
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		r.Detail = "no peer certificates"
		return r
	}
	daysLeft := int(time.Until(certs[0].NotAfter).Hours() / 24)
	if daysLeft < warnDays {
		r.Detail = fmt.Sprintf("expires in %d days", daysLeft)
		return r
	}
	r.OK = true
	r.Detail = fmt.Sprintf("expires in %d days", daysLeft)
	return r
}

func reportFailure(ctx context.Context, cfg config, results []checkResult) error {
	body := buildIssueBody(results)
	existing, err := findOpenOutageIssue(ctx, cfg)
	if err != nil {
		return err
	}
	if existing != 0 {
		return addIssueComment(ctx, cfg, existing, "Still failing:\n\n"+body)
	}
	return createIssue(ctx, cfg, "OUTAGE DETECTED: Production health checks failing", body)
}

func resolveOutage(ctx context.Context, cfg config) error {
	existing, err := findOpenOutageIssue(ctx, cfg)
	if err != nil {
		return err
	}
	if existing == 0 {
		return nil
	}
	if err := addIssueComment(ctx, cfg, existing, fmt.Sprintf("**%s** — Resolved. All health checks passing.", time.Now().UTC().Format(time.RFC3339))); err != nil {
		return err
	}
	return closeIssue(ctx, cfg, existing)
}

func buildIssueBody(results []checkResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**%s** — Health checks failed.\n\n", time.Now().UTC().Format(time.RFC3339))
	b.WriteString("| Check | Status | HTTP | Latency | Details |\n")
	b.WriteString("|-------|--------|------|---------|---------|\n")
	for _, r := range results {
		status := "OK"
		if !r.OK {
			status = "FAIL"
		}
		httpCode := "-"
		if r.Status != 0 {
			httpCode = fmt.Sprintf("%d", r.Status)
		}
		latency := "-"
		if r.Latency > 0 {
			latency = fmt.Sprintf("%dms", r.Latency.Milliseconds())
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", r.Name, status, httpCode, latency, r.Detail)
	}
	return b.String()
}

// --- GitHub API ---

const githubAPI = "https://api.github.com"

func findOpenOutageIssue(ctx context.Context, cfg config) (int, error) {
	u := fmt.Sprintf("%s/repos/%s/issues?state=open&labels=outage&per_page=1", githubAPI, cfg.ghRepo)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	setGHHeaders(req, cfg.ghToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("list issues: %s: %s", resp.Status, string(b))
	}
	var issues []struct {
		Number int `json:"number"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&issues); err != nil {
		return 0, err
	}
	if len(issues) == 0 {
		return 0, nil
	}
	return issues[0].Number, nil
}

func createIssue(ctx context.Context, cfg config, title, body string) error {
	payload, _ := json.Marshal(map[string]any{
		"title":  title,
		"body":   body,
		"labels": []string{"outage"},
	})
	u := fmt.Sprintf("%s/repos/%s/issues", githubAPI, cfg.ghRepo)
	return doGHRequest(ctx, cfg.ghToken, http.MethodPost, u, payload, http.StatusCreated)
}

func addIssueComment(ctx context.Context, cfg config, number int, body string) error {
	payload, _ := json.Marshal(map[string]any{"body": body})
	u := fmt.Sprintf("%s/repos/%s/issues/%d/comments", githubAPI, cfg.ghRepo, number)
	return doGHRequest(ctx, cfg.ghToken, http.MethodPost, u, payload, http.StatusCreated)
}

func closeIssue(ctx context.Context, cfg config, number int) error {
	payload, _ := json.Marshal(map[string]any{
		"state":        "closed",
		"state_reason": "completed",
	})
	u := fmt.Sprintf("%s/repos/%s/issues/%d", githubAPI, cfg.ghRepo, number)
	return doGHRequest(ctx, cfg.ghToken, http.MethodPatch, u, payload, http.StatusOK)
}

func doGHRequest(ctx context.Context, token, method, urlStr string, payload []byte, want int) error {
	req, err := http.NewRequestWithContext(ctx, method, urlStr, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	setGHHeaders(req, token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s: %s: %s", method, urlStr, resp.Status, string(b))
	}
	return nil
}

func setGHHeaders(req *http.Request, token string) {
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "tardi-synthetic-monitor")
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
