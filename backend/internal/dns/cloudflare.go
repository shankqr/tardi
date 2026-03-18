package dns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client manages DNS records via the Cloudflare API.
type Client struct {
	apiToken   string
	zoneID     string
	baseDomain string // e.g. "agents.tardi.ai"
	httpClient *http.Client
}

// NewClient creates a Cloudflare DNS client. Returns nil if config is incomplete.
func NewClient(apiToken, zoneID, baseDomain string) *Client {
	if apiToken == "" || zoneID == "" || baseDomain == "" {
		return nil
	}
	return &Client{
		apiToken:   apiToken,
		zoneID:     zoneID,
		baseDomain: baseDomain,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// BaseDomain returns the configured base domain.
func (c *Client) BaseDomain() string {
	return c.baseDomain
}

// CreateARecord creates an A record pointing subdomain.baseDomain to the given IP.
// Returns the Cloudflare DNS record ID for later deletion.
func (c *Client) CreateARecord(ctx context.Context, subdomain, ip string) (recordID string, err error) {
	fqdn := fmt.Sprintf("%s.%s", subdomain, c.baseDomain)

	body, err := json.Marshal(map[string]any{
		"type":    "A",
		"name":    fqdn,
		"content": ip,
		"ttl":     60,
		"proxied": false,
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records", c.zoneID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("cloudflare API call: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("cloudflare API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Success bool `json:"success"`
		Result  struct {
			ID string `json:"id"`
		} `json:"result"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if !result.Success {
		msg := "unknown error"
		if len(result.Errors) > 0 {
			msg = result.Errors[0].Message
		}
		return "", fmt.Errorf("cloudflare error: %s", msg)
	}

	return result.Result.ID, nil
}

// CreateARecordForDomain creates an A record for an arbitrary FQDN pointing to the given IP.
// Unlike CreateARecord, the domain is not derived from baseDomain.
func (c *Client) CreateARecordForDomain(ctx context.Context, fqdn, ip string) (recordID string, err error) {
	body, err := json.Marshal(map[string]any{
		"type":    "A",
		"name":    fqdn,
		"content": ip,
		"ttl":     60,
		"proxied": false,
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records", c.zoneID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("cloudflare API call: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("cloudflare API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Success bool `json:"success"`
		Result  struct {
			ID string `json:"id"`
		} `json:"result"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if !result.Success {
		msg := "unknown error"
		if len(result.Errors) > 0 {
			msg = result.Errors[0].Message
		}
		return "", fmt.Errorf("cloudflare error: %s", msg)
	}

	return result.Result.ID, nil
}

// UpdateARecordForDomain updates an existing A record for an arbitrary FQDN to a new IP.
func (c *Client) UpdateARecordForDomain(ctx context.Context, recordID, fqdn, ip string) error {
	body, err := json.Marshal(map[string]any{
		"type":    "A",
		"name":    fqdn,
		"content": ip,
		"ttl":     60,
		"proxied": false,
	})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records/%s", c.zoneID, recordID)
	req, err := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare API call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("cloudflare API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// UpdateARecord updates an existing A record to point to a new IP address.
func (c *Client) UpdateARecord(ctx context.Context, recordID, subdomain, ip string) error {
	fqdn := fmt.Sprintf("%s.%s", subdomain, c.baseDomain)

	body, err := json.Marshal(map[string]any{
		"type":    "A",
		"name":    fqdn,
		"content": ip,
		"ttl":     60,
		"proxied": false,
	})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records/%s", c.zoneID, recordID)
	req, err := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare API call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("cloudflare API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// DeleteRecord deletes a DNS record by its Cloudflare record ID.
func (c *Client) DeleteRecord(ctx context.Context, recordID string) error {
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records/%s", c.zoneID, recordID)
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare API call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("cloudflare API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}
