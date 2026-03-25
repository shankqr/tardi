import { check, sleep } from "k6";
import { SMOKE_THRESHOLDS, BASE_URL } from "../config.js";
import { getFirebaseToken, healthz, readyz, getDashboardState, getModels, ensureUser } from "../helpers.js";

export const options = {
  vus: 2,
  duration: "30s",
  thresholds: SMOKE_THRESHOLDS,
};

// Runs once before VUs start — obtain a real Firebase ID token.
export function setup() {
  const token = getFirebaseToken();
  return { token };
}

export default function (data) {
  // Health checks (no auth required)
  let res = healthz();
  check(res, { "healthz returns 200": (r) => r.status === 200 });

  res = readyz();
  check(res, { "readyz returns 200": (r) => r.status === 200 });

  // Public endpoints
  res = getModels();
  check(res, { "models returns 200": (r) => r.status === 200 });

  // Authenticated endpoints
  res = getDashboardState(data.token);
  check(res, {
    "dashboard returns 200": (r) => r.status === 200,
    "dashboard has valid JSON": (r) => {
      try {
        JSON.parse(r.body);
        return true;
      } catch {
        return false;
      }
    },
  });

  sleep(1);
}
