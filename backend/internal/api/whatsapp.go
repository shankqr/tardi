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
// OpenClaw uses a custom protocol: requests are {"type":"req","id":"...","method":"...","params":{...}}
// and responses are {"type":"res","id":"...","ok":true/false,"payload":{...},"error":{...}}.
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
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return nil, fmt.Errorf("set read deadline: %w", err)
	}

	_, msg, err := conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("read challenge: %w", err)
	}

	var event struct {
		Type  string `json:"type"`
		Event string `json:"event"`
	}
	if err := json.Unmarshal(msg, &event); err != nil {
		return nil, fmt.Errorf("parse challenge: %w", err)
	}
	if event.Type != "event" || event.Event != "connect.challenge" {
		return nil, fmt.Errorf("expected connect.challenge, got %s/%s", event.Type, event.Event)
	}

	// Step 2: Send connect request (OpenClaw native protocol)
	connectReq := map[string]any{
		"type":   "req",
		"id":     "connect",
		"method": "connect",
		"params": map[string]any{
			"minProtocol": 3,
			"maxProtocol": 3,
			"client": map[string]any{
				"id":       "openclaw-control-ui",
				"version":  "1.0",
				"platform": "linux",
				"mode":     "webchat",
			},
			"role":   "operator",
			"scopes": []string{"operator.admin", "operator.approvals", "operator.pairing"},
			"caps":   []string{"tool-events"},
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
		Type    string          `json:"type"`
		ID      string          `json:"id"`
		OK      bool            `json:"ok"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(msg, &connectResp); err != nil {
		return nil, fmt.Errorf("parse connect response: %w", err)
	}
	if !connectResp.OK {
		return nil, fmt.Errorf("connect error: %s", string(connectResp.Error))
	}

	// Step 4: Send the actual RPC method
	rpcID := uuid.New().String()
	rpcReq := map[string]any{
		"type":   "req",
		"id":     rpcID,
		"method": method,
		"params": params,
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
			Type    string          `json:"type"`
			ID      string          `json:"id"`
			OK      bool            `json:"ok"`
			Payload json.RawMessage `json:"payload"`
			Error   json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(msg, &resp); err != nil {
			continue // skip malformed messages
		}

		// Skip events, wait for our response
		if resp.Type == "event" {
			continue
		}
		if resp.Type == "res" && resp.ID == rpcID {
			if !resp.OK {
				return nil, fmt.Errorf("rpc error: %s", string(resp.Error))
			}
			return resp.Payload, nil
		}
	}
}

// openclawRPCWithEvents is like openclawRPC but also captures the payload of
// a named event (e.g. "health") that arrives before the RPC response.
// Returns (rpcResult, eventPayload, error). eventPayload is nil if no matching event arrived.
func openclawRPCWithEvents(ctx context.Context, ipv4, authToken, method string, params any, captureEvent string) (json.RawMessage, json.RawMessage, error) {
	url := fmt.Sprintf("wss://%s/?token=%s", ipv4, authToken)

	dialer := websocket.Dialer{
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // Self-signed cert on user's VPS
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("websocket dial: %w", err)
	}
	defer conn.Close()

	// Read connect.challenge
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return nil, nil, fmt.Errorf("set read deadline: %w", err)
	}
	_, msg, err := conn.ReadMessage()
	if err != nil {
		return nil, nil, fmt.Errorf("read challenge: %w", err)
	}
	var challengeEvent struct {
		Type  string `json:"type"`
		Event string `json:"event"`
	}
	if err := json.Unmarshal(msg, &challengeEvent); err != nil || challengeEvent.Event != "connect.challenge" {
		return nil, nil, fmt.Errorf("expected connect.challenge")
	}

	// Send connect
	connectReq := map[string]any{
		"type": "req", "id": "connect", "method": "connect",
		"params": map[string]any{
			"minProtocol": 3, "maxProtocol": 3,
			"client": map[string]any{
				"id": "openclaw-control-ui", "version": "1.0", "platform": "linux", "mode": "webchat",
			},
			"role": "operator", "scopes": []string{"operator.admin", "operator.approvals", "operator.pairing"},
			"caps": []string{"tool-events"},
		},
	}
	if err := conn.WriteJSON(connectReq); err != nil {
		return nil, nil, fmt.Errorf("send connect: %w", err)
	}

	// Read connect response
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return nil, nil, fmt.Errorf("set read deadline: %w", err)
	}
	_, msg, err = conn.ReadMessage()
	if err != nil {
		return nil, nil, fmt.Errorf("read connect response: %w", err)
	}
	var connectResp struct {
		OK    bool            `json:"ok"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(msg, &connectResp); err != nil || !connectResp.OK {
		return nil, nil, fmt.Errorf("connect failed: %s", string(connectResp.Error))
	}

	// Send RPC
	rpcID := uuid.New().String()
	if err := conn.WriteJSON(map[string]any{
		"type": "req", "id": rpcID, "method": method, "params": params,
	}); err != nil {
		return nil, nil, fmt.Errorf("send rpc: %w", err)
	}

	// Read response, capturing the named event if it arrives
	if err := conn.SetReadDeadline(time.Now().Add(35 * time.Second)); err != nil {
		return nil, nil, fmt.Errorf("set read deadline: %w", err)
	}

	var capturedEvent json.RawMessage
	for {
		_, msg, err = conn.ReadMessage()
		if err != nil {
			return nil, nil, fmt.Errorf("read rpc response: %w", err)
		}

		var frame struct {
			Type    string          `json:"type"`
			Event   string          `json:"event"`
			ID      string          `json:"id"`
			OK      bool            `json:"ok"`
			Payload json.RawMessage `json:"payload"`
			Error   json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(msg, &frame); err != nil {
			continue
		}

		if frame.Type == "event" {
			if frame.Event == captureEvent && frame.Payload != nil {
				capturedEvent = frame.Payload
			}
			continue
		}
		if frame.Type == "res" && frame.ID == rpcID {
			if !frame.OK {
				return nil, nil, fmt.Errorf("rpc error: %s", string(frame.Error))
			}
			return frame.Payload, capturedEvent, nil
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

		force := r.URL.Query().Get("force") == "true"

		result, err := openclawRPC(ctx, *inst.IPv4, *inst.OpenClawAuthToken, "web.login.start", map[string]any{
			"force":     force,
			"timeoutMs": 30000,
		})
		if err != nil {
			slog.Error("whatsapp qr: rpc failed", "error", err, "instance_id", instanceID)
			WriteError(w, http.StatusBadGateway, "gateway_error", "failed to get WhatsApp QR code")
			return
		}

		slog.Info("whatsapp qr: raw response",
			"data_preview", string(result)[:min(500, len(result))],
			"instance_id", instanceID,
		)

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

		// channels.status with probe=true returns cached data in the "res" message,
		// but fires a fresh "health" event with live channel state. We use
		// openclawRPCWithEvents to capture that health event for accurate status.
		result, healthEvent, err := openclawRPCWithEvents(ctx, *inst.IPv4, *inst.OpenClawAuthToken, "channels.status", map[string]any{
			"probe":     true,
			"timeoutMs": 10000,
		}, "health")
		if err != nil {
			slog.Error("whatsapp status: rpc failed", "error", err, "instance_id", instanceID)
			WriteError(w, http.StatusBadGateway, "gateway_error", "failed to get WhatsApp status")
			return
		}

		// Prefer health event data (fresh probe) over response data (cached)
		statusData := result
		source := "res"
		if healthEvent != nil {
			statusData = healthEvent
			source = "health-event"
		}

		slog.Info("whatsapp status: raw data",
			"source", source,
			"has_health_event", healthEvent != nil,
			"data_preview", string(statusData)[:min(500, len(statusData))],
			"instance_id", instanceID,
		)

		var status struct {
			Channels struct {
				WhatsApp *struct {
					Linked    bool `json:"linked"`
					Connected bool `json:"connected"`
					Running   bool `json:"running"`
					Self      struct {
						E164 *string `json:"e164"`
					} `json:"self"`
				} `json:"whatsapp"`
			} `json:"channels"`
		}
		if err := json.Unmarshal(statusData, &status); err != nil {
			slog.Error("whatsapp status: unmarshal failed", "error", err, "instance_id", instanceID)
			WriteJSON(w, http.StatusOK, map[string]any{
				"linked": false,
			})
			return
		}

		linked := false
		phone := ""
		if status.Channels.WhatsApp != nil {
			// "linked" in OpenClaw means "has stored auth credentials" which persists
			// even after the user unlinks from their phone. We require both linked AND
			// (connected OR running) to consider it actually working.
			wa := status.Channels.WhatsApp
			linked = wa.Linked && (wa.Connected || wa.Running)
			if wa.Self.E164 != nil {
				phone = *wa.Self.E164
			}
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"linked": linked,
			"phone":  phone,
		})
	}
}
