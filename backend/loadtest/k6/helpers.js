import http from "k6/http";
import { BASE_URL } from "./config.js";

// Generate a unique auth token per VU. In dev mode, the backend treats
// arbitrary bearer tokens as Firebase UIDs, auto-creating users.
export function authHeaders(vuId) {
  return {
    headers: {
      Authorization: `Bearer loadtest-user-${vuId}`,
      "Content-Type": "application/json",
    },
  };
}

// API request helpers

export function getDashboardState(vuId) {
  return http.get(`${BASE_URL}/api/dashboard/state`, authHeaders(vuId));
}

export function createInstance(vuId, name, region) {
  return http.post(
    `${BASE_URL}/api/instances`,
    JSON.stringify({ name: name || "loadtest-agent", region: region || "eu-central" }),
    authHeaders(vuId),
  );
}

export function deleteInstance(vuId, instanceId) {
  return http.del(`${BASE_URL}/api/instances/${instanceId}`, null, authHeaders(vuId));
}

export function restartInstance(vuId, instanceId) {
  return http.post(`${BASE_URL}/api/instances/${instanceId}/restart`, null, authHeaders(vuId));
}

export function createSnapshot(vuId, instanceId, name) {
  return http.post(
    `${BASE_URL}/api/instances/${instanceId}/snapshots`,
    JSON.stringify({ name: name || "loadtest-snap" }),
    authHeaders(vuId),
  );
}

export function getModels() {
  return http.get(`${BASE_URL}/api/models`);
}

export function healthz() {
  return http.get(`${BASE_URL}/healthz`);
}

export function readyz() {
  return http.get(`${BASE_URL}/readyz`);
}

export function sendHeartbeat(agentToken, status) {
  return http.post(
    `${BASE_URL}/api/agent/heartbeat`,
    JSON.stringify({
      status: status || "healthy",
      openclaw_version: "1.0.0-loadtest",
    }),
    {
      headers: {
        Authorization: `Bearer ${agentToken}`,
        "Content-Type": "application/json",
      },
    },
  );
}

export function billingPortal(vuId) {
  return http.post(`${BASE_URL}/api/billing/portal`, null, authHeaders(vuId));
}

// Setup helper: ensure a user has an active subscription.
// In dev mode, we can create subscriptions by simulating the checkout webhook.
// For simplicity, this seeds directly via the dashboard endpoint (which auto-creates users).
export function ensureUser(vuId) {
  const res = getDashboardState(vuId);
  return res.status === 200;
}
