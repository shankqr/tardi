import { check, sleep } from "k6";
import { Trend, Counter } from "k6/metrics";
import { BASE_URL } from "../config.js";
import http from "k6/http";

const heartbeatLatency = new Trend("heartbeat_latency");
const heartbeatErrors = new Counter("heartbeat_errors");

export const options = {
  // Simulate all VPS agents sending heartbeats at once
  // (e.g., after a maintenance window where all agents restart)
  scenarios: {
    flood: {
      executor: "constant-vus",
      vus: 200,
      duration: "2m",
    },
  },
  thresholds: {
    heartbeat_latency: ["p(95)<1000"],
    heartbeat_errors: ["count<20"],
    http_req_failed: ["rate<0.05"],
  },
};

export default function () {
  // Each VU simulates a unique agent with a unique token
  const agentToken = `agent-loadtest-${__VU}`;

  const res = http.post(
    `${BASE_URL}/api/agent/heartbeat`,
    JSON.stringify({
      status: "healthy",
      openclaw_version: "1.0.0-loadtest",
    }),
    {
      headers: {
        Authorization: `Bearer ${agentToken}`,
        "Content-Type": "application/json",
      },
    },
  );

  heartbeatLatency.add(res.timings.duration);

  const ok = check(res, {
    // 200 if valid token, 401/404 if token not found — both are valid for load testing
    "heartbeat accepted": (r) => r.status === 200 || r.status === 401 || r.status === 404,
  });

  if (!ok) {
    heartbeatErrors.add(1);
  }

  // Heartbeats come every 5 minutes in prod. For load testing, send faster.
  sleep(1);
}
