package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/shanq/tardi/internal/api/middleware"
	"github.com/shanq/tardi/internal/db"
)

// openclawRPC connects to an OpenClaw gateway via WebSocket (through Caddy),
// handles the connect handshake, sends an RPC method, and returns the result.
func openclawRPC(ctx context.Context, ipv4, authToken, method string, params any) (json.RawMessage, error) {
	url := fmt.Sprintf("wss://%s/?token=%s", ipv4, authToken)

	dialer := websocket.Dialer{
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // Self-signed cert on user's VPS
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("websocket dial: %w", err)
	}
	defer conn.Close()

	// Step 1: Read connect.challenge event
	var challengeNonce string
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return nil, fmt.Errorf("set read deadline: %w", err)
	}

	_, msg, err := conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("read challenge: %w", err)
	}

	var event struct {
		Type    string `json:"type"`
		Event   string `json:"event"`
		Payload struct {
			Nonce string `json:"nonce"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(msg, &event); err != nil {
		return nil, fmt.Errorf("parse challenge: %w", err)
	}
	if event.Event == "connect.challenge" {
		challengeNonce = event.Payload.Nonce
	}

	// Step 2: Send connect RPC
	connectReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      "connect",
		"method":  "connect",
		"params": map[string]any{
			"minProtocol": 3,
			"maxProtocol": 3,
			"client": map[string]any{
				"id":       "tardi-backend",
				"version":  "1.0",
				"platform": "linux",
				"mode":     "webchat",
			},
			"role":   "operator",
			"scopes": []string{"operator.admin", "operator.approvals", "operator.pairing"},
			"caps":   []string{"tool-events"},
			"nonce":  challengeNonce,
		},
	}
	if err := conn.WriteJSON(connectReq); err != nil {
		return nil, fmt.Errorf("send connect: %w", err)
	}

	// Step 3: Read connect response
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return nil, fmt.Errorf("set read deadline: %w", err)
	}

	_, msg, err = conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("read connect response: %w", err)
	}

	var connectResp struct {
		Type  string          `json:"type"`
		ID    string          `json:"id"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(msg, &connectResp); err != nil {
		return nil, fmt.Errorf("parse connect response: %w", err)
	}
	if connectResp.Error != nil && string(connectResp.Error) != "null" {
		return nil, fmt.Errorf("connect error: %s", string(connectResp.Error))
	}

	// Step 4: Send the actual RPC method
	rpcID := uuid.New().String()
	rpcReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      rpcID,
		"method":  method,
		"params":  params,
	}
	if err := conn.WriteJSON(rpcReq); err != nil {
		return nil, fmt.Errorf("send rpc: %w", err)
	}

	// Step 5: Read RPC response (skip events)
	deadline := time.Now().Add(35 * time.Second)
	if err := conn.SetReadDeadline(deadline); err != nil {
		return nil, fmt.Errorf("set read deadline: %w", err)
	}

	for {
		_, msg, err = conn.ReadMessage()
		if err != nil {
			return nil, fmt.Errorf("read rpc response: %w", err)
		}

		var resp struct {
			Type   string          `json:"type"`
			ID     string          `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(msg, &resp); err != nil {
			continue // skip malformed messages
		}

		// Skip events, wait for our response
		if resp.Type == "event" {
			continue
		}
		if resp.ID == rpcID {
			if resp.Error != nil && string(resp.Error) != "null" {
				return nil, fmt.Errorf("rpc error: %s", string(resp.Error))
			}
			return resp.Result, nil
		}
	}
}

// WhatsAppQRHandler returns the WhatsApp login QR code from OpenClaw.
// POST /api/instances/{id}/whatsapp/qr
func WhatsAppQRHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
			return
		}

		instanceID, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid instance id")
			return
		}

		inst, err := db.GetInstanceByID(r.Context(), deps.Pool, instanceID, user.ID)
		if err != nil {
			WriteError(w, http.StatusNotFound, "not_found", "instance not found")
			return
		}

		if inst.IPv4 == nil || inst.OpenClawAuthToken == nil {
			WriteError(w, http.StatusConflict, "not_ready", "instance not ready")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
		defer cancel()

		result, err := openclawRPC(ctx, *inst.IPv4, *inst.OpenClawAuthToken, "web.login.start", map[string]any{
			"force":     false,
			"timeoutMs": 30000,
		})
		if err != nil {
			slog.Error("whatsapp qr: rpc failed", "error", err, "instance_id", instanceID)
			WriteError(w, http.StatusBadGateway, "gateway_error", "failed to get WhatsApp QR code")
			return
		}

		var qrResult struct {
			QRDataURL string `json:"qrDataUrl"`
			Message   string `json:"message"`
		}
		if err := json.Unmarshal(result, &qrResult); err != nil {
			WriteError(w, http.StatusBadGateway, "gateway_error", "invalid response from agent")
			return
		}

		WriteJSON(w, http.StatusOK, map[string]string{
			"qr_data_url": qrResult.QRDataURL,
			"message":     qrResult.Message,
		})
	}
}

// WhatsAppStatusHandler returns the WhatsApp connection status from OpenClaw.
// GET /api/instances/{id}/whatsapp/status
func WhatsAppStatusHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
			return
		}

		instanceID, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid instance id")
			return
		}

		inst, err := db.GetInstanceByID(r.Context(), deps.Pool, instanceID, user.ID)
		if err != nil {
			WriteError(w, http.StatusNotFound, "not_found", "instance not found")
			return
		}

		if inst.IPv4 == nil || inst.OpenClawAuthToken == nil {
			WriteError(w, http.StatusConflict, "not_ready", "instance not ready")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()

		result, err := openclawRPC(ctx, *inst.IPv4, *inst.OpenClawAuthToken, "channels.status", map[string]any{})
		if err != nil {
			slog.Error("whatsapp status: rpc failed", "error", err, "instance_id", instanceID)
			WriteError(w, http.StatusBadGateway, "gateway_error", "failed to get WhatsApp status")
			return
		}

		var status struct {
			Channels struct {
				WhatsApp *struct {
					Accounts map[string]struct {
						Connected bool   `json:"connected"`
						Phone     string `json:"phone"`
					} `json:"accounts"`
				} `json:"whatsapp"`
			} `json:"channels"`
		}
		if err := json.Unmarshal(result, &status); err != nil {
			WriteJSON(w, http.StatusOK, map[string]any{
				"linked": false,
			})
			return
		}

		linked := false
		phone := ""
		if status.Channels.WhatsApp != nil {
			for _, acct := range status.Channels.WhatsApp.Accounts {
				if acct.Connected {
					linked = true
					phone = acct.Phone
					break
				}
			}
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"linked": linked,
			"phone":  phone,
		})
	}
}
