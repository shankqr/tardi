import { check, sleep } from "k6";
import { Trend, Counter } from "k6/metrics";
import { DEFAULT_THRESHOLDS } from "../config.js";
import { getFirebaseToken, getDashboardState } from "../helpers.js";

const dashboardLatency = new Trend("dashboard_p95");
const timeouts = new Counter("dashboard_timeouts");

export const options = {
  // Ramp from 10 to 500 VUs to find the pool saturation inflection point
  stages: [
    { duration: "30s", target: 10 },   // Warm up
    { duration: "1m", target: 50 },    // Light load
    { duration: "1m", target: 100 },   // Normal load
    { duration: "1m", target: 200 },   // Heavy load
    { duration: "1m", target: 500 },   // Stress — expect pool saturation
    { duration: "30s", target: 10 },   // Cool down
  ],
  thresholds: {
    dashboard_p95: ["p(95)<3000"],     // Dev infra (db-f1-micro) saturates at high VUs
    dashboard_timeouts: ["count<200"], // Some timeouts expected under extreme load
    http_req_failed: ["rate<0.15"],    // Allow up to 15% failures at peak on dev
  },
};

// Runs once before VUs start — obtain a real Firebase ID token.
export function setup() {
  const token = getFirebaseToken();
  return { token };
}

export default function (data) {
  const res = getDashboardState(data.token);

  dashboardLatency.add(res.timings.duration);

  const ok = check(res, {
    "status 200": (r) => r.status === 200,
    "under 1s": (r) => r.timings.duration < 1000,
  });

  if (res.timings.duration > 5000) {
    timeouts.add(1);
  }

  sleep(5); // Match frontend polling interval
}
