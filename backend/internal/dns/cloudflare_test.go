package dns

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewClient_AllConfig(t *testing.T) {
	c := NewClient("token", "zone-id", "agents.tardi.ai")
	if c == nil {
		t.Fatal("NewClient returned nil with all config")
	}
	if c.apiToken != "token" {
		t.Errorf("apiToken = %q", c.apiToken)
	}
	if c.zoneID != "zone-id" {
		t.Errorf("zoneID = %q", c.zoneID)
	}
	if c.baseDomain != "agents.tardi.ai" {
		t.Errorf("baseDomain = %q", c.baseDomain)
	}
}

func TestNewClient_MissingToken(t *testing.T) {
	c := NewClient("", "zone-id", "agents.tardi.ai")
	if c != nil {
		t.Error("NewClient should return nil when apiToken is empty")
	}
}

func TestNewClient_MissingZoneID(t *testing.T) {
	c := NewClient("token", "", "agents.tardi.ai")
	if c != nil {
		t.Error("NewClient should return nil when zoneID is empty")
	}
}

func TestNewClient_MissingBaseDomain(t *testing.T) {
	c := NewClient("token", "zone-id", "")
	if c != nil {
		t.Error("NewClient should return nil when baseDomain is empty")
	}
}

func TestNewClient_AllEmpty(t *testing.T) {
	c := NewClient("", "", "")
	if c != nil {
		t.Error("NewClient should return nil when all config is empty")
	}
}

func TestBaseDomain(t *testing.T) {
	c := NewClient("token", "zone-id", "agents.tardi.ai")
	if c.BaseDomain() != "agents.tardi.ai" {
		t.Errorf("BaseDomain() = %q, want %q", c.BaseDomain(), "agents.tardi.ai")
	}
}

func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	c := &Client{
		apiToken:   "test-token",
		zoneID:     "test-zone",
		baseDomain: "agents.tardi.ai",
		httpClient: srv.Client(),
	}
	return c, srv
}

func TestCreateARecord_Success(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/dns_records") {
			t.Errorf("path = %q, expected /dns_records", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}

		// Verify body
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["type"] != "A" {
			t.Errorf("type = %v", body["type"])
		}
		if body["name"] != "myagent.agents.tardi.ai" {
			t.Errorf("name = %v", body["name"])
		}
		if body["content"] != "1.2.3.4" {
			t.Errorf("content = %v", body["content"])
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result":  map[string]any{"id": "rec-123"},
		})
	})

	c, srv := newTestClient(t, handler)
	defer srv.Close()

	// Override the URL by patching the zoneID to include the server URL
	// We need to make the client hit our test server instead of Cloudflare
	origZoneID := c.zoneID
	c.zoneID = "test-zone"
	_ = origZoneID

	// Since the client hardcodes the Cloudflare URL, we need a different approach:
	// create a client that uses a custom HTTP client with transport rewriting
	c.httpClient = srv.Client()
	c.httpClient.Transport = &rewriteTransport{
		base:    srv.Client().Transport,
		baseURL: srv.URL,
	}

	recordID, err := c.CreateARecord(context.Background(), "myagent", "1.2.3.4")
	if err != nil {
		t.Fatalf("CreateARecord: %v", err)
	}
	if recordID != "rec-123" {
		t.Errorf("recordID = %q, want %q", recordID, "rec-123")
	}
}

func TestCreateARecord_APIError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	})

	c, srv := newTestClient(t, handler)
	defer srv.Close()
	c.httpClient.Transport = &rewriteTransport{
		base:    srv.Client().Transport,
		baseURL: srv.URL,
	}

	_, err := c.CreateARecord(context.Background(), "myagent", "1.2.3.4")
	if err == nil {
		t.Fatal("expected error for API error response")
	}
	if !strings.Contains(err.Error(), "cloudflare API error") {
		t.Errorf("error = %q, expected cloudflare API error", err.Error())
	}
}

func TestCreateARecord_CloudflareError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"errors":  []map[string]any{{"message": "record already exists"}},
		})
	})

	c, srv := newTestClient(t, handler)
	defer srv.Close()
	c.httpClient.Transport = &rewriteTransport{
		base:    srv.Client().Transport,
		baseURL: srv.URL,
	}

	_, err := c.CreateARecord(context.Background(), "myagent", "1.2.3.4")
	if err == nil {
		t.Fatal("expected error for cloudflare error response")
	}
	if !strings.Contains(err.Error(), "record already exists") {
		t.Errorf("error = %q, expected 'record already exists'", err.Error())
	}
}

func TestDeleteRecord_Success(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("method = %q, want DELETE", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/dns_records/rec-456") {
			t.Errorf("path = %q, expected /dns_records/rec-456", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	})

	c, srv := newTestClient(t, handler)
	defer srv.Close()
	c.httpClient.Transport = &rewriteTransport{
		base:    srv.Client().Transport,
		baseURL: srv.URL,
	}

	err := c.DeleteRecord(context.Background(), "rec-456")
	if err != nil {
		t.Fatalf("DeleteRecord: %v", err)
	}
}

func TestDeleteRecord_APIError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	})

	c, srv := newTestClient(t, handler)
	defer srv.Close()
	c.httpClient.Transport = &rewriteTransport{
		base:    srv.Client().Transport,
		baseURL: srv.URL,
	}

	err := c.DeleteRecord(context.Background(), "rec-456")
	if err == nil {
		t.Fatal("expected error for 404")
	}
}

func TestUpdateARecord_Success(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			t.Errorf("method = %q, want PUT", r.Method)
		}

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "myagent.agents.tardi.ai" {
			t.Errorf("name = %v", body["name"])
		}
		if body["content"] != "5.6.7.8" {
			t.Errorf("content = %v", body["content"])
		}

		w.WriteHeader(http.StatusOK)
	})

	c, srv := newTestClient(t, handler)
	defer srv.Close()
	c.httpClient.Transport = &rewriteTransport{
		base:    srv.Client().Transport,
		baseURL: srv.URL,
	}

	err := c.UpdateARecord(context.Background(), "rec-789", "myagent", "5.6.7.8")
	if err != nil {
		t.Fatalf("UpdateARecord: %v", err)
	}
}

func TestUpdateARecordForDomain_Success(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			t.Errorf("method = %q, want PUT", r.Method)
		}

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "custom.tardi.ai" {
			t.Errorf("name = %v", body["name"])
		}

		w.WriteHeader(http.StatusOK)
	})

	c, srv := newTestClient(t, handler)
	defer srv.Close()
	c.httpClient.Transport = &rewriteTransport{
		base:    srv.Client().Transport,
		baseURL: srv.URL,
	}

	err := c.UpdateARecordForDomain(context.Background(), "rec-aaa", "custom.tardi.ai", "9.8.7.6")
	if err != nil {
		t.Fatalf("UpdateARecordForDomain: %v", err)
	}
}

func TestCreateARecordForDomain_Success(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "custom.example.com" {
			t.Errorf("name = %v, want custom.example.com", body["name"])
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result":  map[string]any{"id": "rec-custom"},
		})
	})

	c, srv := newTestClient(t, handler)
	defer srv.Close()
	c.httpClient.Transport = &rewriteTransport{
		base:    srv.Client().Transport,
		baseURL: srv.URL,
	}

	recordID, err := c.CreateARecordForDomain(context.Background(), "custom.example.com", "1.1.1.1")
	if err != nil {
		t.Fatalf("CreateARecordForDomain: %v", err)
	}
	if recordID != "rec-custom" {
		t.Errorf("recordID = %q, want %q", recordID, "rec-custom")
	}
}

// rewriteTransport rewrites all requests to point to the test server.
type rewriteTransport struct {
	base    http.RoundTripper
	baseURL string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Rewrite the URL to point to our test server, preserving the path
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(t.baseURL, "http://")
	if t.base != nil {
		return t.base.RoundTrip(req)
	}
	return http.DefaultTransport.RoundTrip(req)
}
