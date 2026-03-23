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

// openclawRPCKeepAlive is like openclawRPC but keeps the WebSocket connection
// alive in a background goroutine for keepAliveDuration after receiving the RPC
// response. This is needed for web.login.start because OpenClaw cancels the
// pending WhatsApp login session when the operator WebSocket disconnects.
func openclawRPCKeepAlive(ctx context.Context, ipv4, authToken, method string, params any, keepAliveDuration time.Duration, instanceID string) (json.RawMessage, error) {
	url := fmt.Sprintf("wss://%s/?token=%s", ipv4, authToken)

	dialer := websocket.Dialer{
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // Self-signed cert on user's VPS
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("websocket dial: %w", err)
	}
	// NOTE: no defer conn.Close() — connection is closed by the background goroutine

	// Step 1: Read connect.challenge event
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set read deadline: %w", err)
	}

	_, msg, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read challenge: %w", err)
	}

	var event struct {
		Type  string `json:"type"`
		Event string `json:"event"`
	}
	if err := json.Unmarshal(msg, &event); err != nil {
		conn.Close()
		return nil, fmt.Errorf("parse challenge: %w", err)
	}
	if event.Type != "event" || event.Event != "connect.challenge" {
		conn.Close()
		return nil, fmt.Errorf("expected connect.challenge, got %s/%s", event.Type, event.Event)
	}

	// Step 2: Send connect request
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
			"scopes": []string{"operator.read", "operator.write", "operator.admin", "operator.approvals", "operator.pairing"},
			"caps":   []string{"tool-events"},
		},
	}
	if err := conn.WriteJSON(connectReq); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send connect: %w", err)
	}

	// Step 3: Read connect response
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set read deadline: %w", err)
	}

	_, msg, err = conn.ReadMessage()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read connect response: %w", err)
	}

	var connectResp struct {
		Type  string          `json:"type"`
		ID    string          `json:"id"`
		OK    bool            `json:"ok"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(msg, &connectResp); err != nil {
		conn.Close()
		return nil, fmt.Errorf("parse connect response: %w", err)
	}
	if !connectResp.OK {
		conn.Close()
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
		conn.Close()
		return nil, fmt.Errorf("send rpc: %w", err)
	}

	// Step 5: Read RPC response (skip events)
	deadline := time.Now().Add(35 * time.Second)
	if err := conn.SetReadDeadline(deadline); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set read deadline: %w", err)
	}

	var result json.RawMessage
	for {
		_, msg, err = conn.ReadMessage()
		if err != nil {
			conn.Close()
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
			continue
		}

		if resp.Type == "event" {
			continue
		}
		if resp.Type == "res" && resp.ID == rpcID {
			if !resp.OK {
				conn.Close()
				return nil, fmt.Errorf("rpc error: %s", string(resp.Error))
			}
			result = resp.Payload
			break
		}
	}

	// Step 6: Keep the WebSocket alive in a background goroutine so OpenClaw
	// doesn't cancel the login session. Read and discard any events.
	go func() {
		slog.Info("whatsapp qr: keeping WebSocket alive for QR scan window",
			"duration", keepAliveDuration.String(),
			"instance_id", instanceID,
		)
		_ = conn.SetReadDeadline(time.Now().Add(keepAliveDuration))
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
		conn.Close()
		slog.Info("whatsapp qr: background WebSocket closed",
			"instance_id", instanceID,
		)
	}()

	return result, nil
}

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
			"scopes": []string{"operator.read", "operator.write", "operator.admin", "operator.approvals", "operator.pairing"},
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
			"role": "operator", "scopes": []string{"operator.read", "operator.write", "operator.admin", "operator.approvals", "operator.pairing"},
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

		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()

		force := r.URL.Query().Get("force") == "true"

		// Use config.get → config.patch to ensure WhatsApp and web channels are enabled.
		// config.patch requires a base hash from config.get for optimistic concurrency.
		configGetResult, configGetErr := openclawRPC(ctx, *inst.IPv4, *inst.OpenClawAuthToken, "config.get", map[string]any{})
		if configGetErr != nil {
			slog.Warn("whatsapp qr: config.get failed", "error", configGetErr, "instance_id", instanceID)
		} else {
			// Extract the hash from config.get response
			var configResp struct {
				Hash string `json:"hash"`
			}
			if err := json.Unmarshal(configGetResult, &configResp); err == nil && configResp.Hash != "" {
				patchJSON := `{"channels":{"web":{"enabled":true},"whatsapp":{"enabled":true,"dmPolicy":"pairing","groupPolicy":"disabled"}}}`
				patchResult, patchErr := openclawRPC(ctx, *inst.IPv4, *inst.OpenClawAuthToken, "config.patch", map[string]any{
					"raw":  patchJSON,
					"hash": configResp.Hash,
				})
				if patchErr != nil {
					slog.Warn("whatsapp qr: config.patch failed",
						"error", patchErr,
						"instance_id", instanceID,
					)
				} else {
					slog.Info("whatsapp qr: config.patch succeeded",
						"result", string(patchResult),
						"instance_id", instanceID,
					)
					// Give gateway time to restart after config change
					time.Sleep(5 * time.Second)
				}
			} else {
				slog.Warn("whatsapp qr: config.get response missing hash",
					"raw", string(configGetResult),
					"instance_id", instanceID,
				)
			}
		}

		// Log channel status for debugging
		statusResult, healthEvent, statusErr := openclawRPCWithEvents(ctx, *inst.IPv4, *inst.OpenClawAuthToken, "channels.status", map[string]any{
			"probe":     true,
			"timeoutMs": 5000,
		}, "health")
		if statusErr == nil {
			checkData := statusResult
			if healthEvent != nil {
				checkData = healthEvent
			}
			var statusCheck struct {
				Channels map[string]json.RawMessage `json:"channels"`
			}
			if err := json.Unmarshal(checkData, &statusCheck); err == nil {
				keys := make([]string, 0, len(statusCheck.Channels))
				for k := range statusCheck.Channels {
					keys = append(keys, k)
				}
				slog.Info("whatsapp qr: post-patch channel status",
					"channel_keys", keys,
					"instance_id", instanceID,
				)
				for name, data := range statusCheck.Channels {
					slog.Info("whatsapp qr: channel detail",
						"channel", name,
						"data", string(data),
						"instance_id", instanceID,
					)
				}
			}
		}

		// Note: we do NOT call channels.logout on force reconnect — it deconfigures
		// the channel entirely (sets configured=false). Instead, web.login.start with
		// force=true handles clearing stale sessions natively.

		// Use openclawRPCKeepAlive to get the QR code while keeping the WebSocket
		// alive in the background. OpenClaw cancels the login session when the
		// operator WebSocket disconnects, so we hold it open for 35s to allow
		// the user time to scan.
		result, err := openclawRPCKeepAlive(ctx, *inst.IPv4, *inst.OpenClawAuthToken, "web.login.start", map[string]any{
			"force":     force,
			"timeoutMs": 30000,
		}, 35*time.Second, instanceID.String())
		if err != nil {
			slog.Error("whatsapp qr: rpc failed", "error", err, "instance_id", instanceID)
			WriteError(w, http.StatusBadGateway, "gateway_error", "failed to get WhatsApp QR code")
			return
		}

		// Log response fields for debugging
		var rawResult map[string]json.RawMessage
		if err := json.Unmarshal(result, &rawResult); err == nil {
			keys := make([]string, 0, len(rawResult))
			for k := range rawResult {
				keys = append(keys, k)
			}
			hasQR := false
			qrLen := 0
			if qr, ok := rawResult["qrDataUrl"]; ok {
				hasQR = true
				qrLen = len(qr)
			}
			slog.Info("whatsapp qr: response fields",
				"keys", keys,
				"has_qr", hasQR,
				"qr_length", qrLen,
				"instance_id", instanceID,
			)
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
			"data_length", len(statusData),
			"instance_id", instanceID,
		)

		// Parse outer structure to extract just the WhatsApp channel data
		var outer struct {
			Channels map[string]json.RawMessage `json:"channels"`
		}
		if err := json.Unmarshal(statusData, &outer); err != nil {
			slog.Error("whatsapp status: unmarshal outer failed", "error", err, "instance_id", instanceID)
			WriteJSON(w, http.StatusOK, map[string]any{"linked": false})
			return
		}

		// Log all channel keys present
		channelKeys := make([]string, 0, len(outer.Channels))
		for k := range outer.Channels {
			channelKeys = append(channelKeys, k)
		}
		slog.Info("whatsapp status: channels present",
			"keys", channelKeys,
			"instance_id", instanceID,
		)

		// Log full WhatsApp channel data (untruncated)
		if waData, ok := outer.Channels["whatsapp"]; ok {
			slog.Info("whatsapp status: whatsapp channel data",
				"data", string(waData),
				"instance_id", instanceID,
			)
		} else {
			slog.Warn("whatsapp status: NO 'whatsapp' key in channels",
				"available_keys", channelKeys,
				"instance_id", instanceID,
			)
		}

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
			wa := status.Channels.WhatsApp
			linked = wa.Linked
			if wa.Self.E164 != nil {
				phone = *wa.Self.E164
			}
			slog.Info("whatsapp status: parsed fields",
				"wa_linked", wa.Linked,
				"wa_connected", wa.Connected,
				"wa_running", wa.Running,
				"result_linked", linked,
				"phone", phone,
				"instance_id", instanceID,
			)

			// Log warning if linked but not running — gateway should auto-connect
			if wa.Linked && !wa.Running {
				slog.Warn("whatsapp status: linked but not running — gateway reconnect loop should handle this",
					"instance_id", instanceID,
				)
			}
		} else {
			slog.Warn("whatsapp status: WhatsApp struct is nil after unmarshal",
				"instance_id", instanceID,
			)
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"linked": linked,
			"phone":  phone,
		})
	}
}
