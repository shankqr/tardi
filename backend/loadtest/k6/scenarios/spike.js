import { check, sleep } from "k6";
import { Trend } from "k6/metrics";
import { getDashboardState, healthz } from "../helpers.js";

const recoveryLatency = new Trend("post_spike_latency");

export const options = {
  stages: [
    { duration: "30s", target: 5 },    // Normal baseline
    { duration: "5s", target: 100 },   // SPIKE: instant jump
    { duration: "1m", target: 100 },   // Sustain spike
    { duration: "5s", target: 5 },     // Drop back
    { duration: "2m", target: 5 },     // Recovery observation
    { duration: "30s", target: 0 },    // Ramp down
  ],
  thresholds: {
    http_req_duration: ["p(95)<2000"],          // Allow high latency during spike
    post_spike_latency: ["p(95)<500"],          // Must recover quickly
    http_req_failed: ["rate<0.1"],              // Allow some failures during spike
  },
};

export default function () {
  const vuId = `spike-${__VU}`;

  const res = getDashboardState(vuId);
  check(res, {
    "response received": (r) => r.status === 200 || r.status === 429,
  });

  // Track post-spike latency (after the first 2 minutes)
  const elapsed = new Date() - __ENV.K6_START_TIME;
  if (elapsed > 120000) {
    // We're in the recovery phase
    recoveryLatency.add(res.timings.duration);
  }

  sleep(1);
}
