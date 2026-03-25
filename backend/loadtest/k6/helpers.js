import http from "k6/http";
import { BASE_URL, FIREBASE_API_KEY, FIREBASE_EMAIL, FIREBASE_PASSWORD } from "./config.js";

// Obtain a real Firebase ID token via the REST API.
// Called once in setup() and shared across all VUs.
export function getFirebaseToken() {
  if (!FIREBASE_API_KEY || !FIREBASE_EMAIL || !FIREBASE_PASSWORD) {
    throw new Error(
      "Missing FIREBASE_API_KEY, FIREBASE_EMAIL, or FIREBASE_PASSWORD env vars. " +
      "Required for authenticated load tests against deployed environments."
    );
  }

  const res = http.post(
    `https://identitytoolkit.googleapis.com/v1/accounts:signInWithPassword?key=${FIREBASE_API_KEY}`,
    JSON.stringify({
      email: FIREBASE_EMAIL,
      password: FIREBASE_PASSWORD,
      returnSecureToken: true,
    }),
    { headers: { "Content-Type": "application/json" } },
  );

  if (res.status !== 200) {
    throw new Error(`Firebase auth failed (${res.status}): ${res.body}`);
  }

  const body = JSON.parse(res.body);
  return body.idToken;
}

// Build auth headers using a real Firebase token (from setup) or a VU-specific
// fake token (for local dev mode where Firebase is not initialised).
export function authHeaders(tokenOrVuId) {
  const token = tokenOrVuId.startsWith && tokenOrVuId.startsWith("ey")
    ? tokenOrVuId
    : `loadtest-user-${tokenOrVuId}`;
  return {
    headers: {
      Authorization: `Bearer ${token}`,
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
