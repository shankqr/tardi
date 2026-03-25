import { check, sleep, group } from "k6";
import { Counter, Trend } from "k6/metrics";
import { DEFAULT_THRESHOLDS, BASE_URL } from "../config.js";
import {
  getFirebaseToken,
  getDashboardState,
  createInstance,
  deleteInstance,
  getModels,
  healthz,
} from "../helpers.js";

const dashboardLatency = new Trend("dashboard_latency");
const provisioningErrors = new Counter("provisioning_errors");

export const options = {
  scenarios: {
    // 50 users polling the dashboard every 5 seconds
    dashboard_pollers: {
      executor: "constant-vus",
      vus: 50,
      duration: "10m",
      exec: "dashboardPoller",
    },
    // 5 users going through the provisioning lifecycle
    provisioners: {
      executor: "constant-vus",
      vus: 5,
      duration: "10m",
      exec: "provisioningLifecycle",
      startTime: "10s", // slight delay to let pollers warm up
    },
    // 10 agents sending heartbeats
    heartbeats: {
      executor: "constant-vus",
      vus: 10,
      duration: "10m",
      exec: "heartbeatAgent",
      startTime: "5s",
    },
  },
  thresholds: {
    ...DEFAULT_THRESHOLDS,
    dashboard_latency: ["p(95)<300"],
  },
};

// Runs once before VUs start — obtain a real Firebase ID token.
export function setup() {
  const token = getFirebaseToken();
  return { token };
}

export function dashboardPoller(data) {
  const res = getDashboardState(data.token);
  dashboardLatency.add(res.timings.duration);

  check(res, {
    "dashboard 200": (r) => r.status === 200,
  });

  sleep(5); // Match frontend polling interval
}

export function provisioningLifecycle(data) {
  group("create instance", () => {
    const res = createInstance(data.token, `load-test-agent-${__VU}`, "eu-central");
    const success = check(res, {
      "create returns 201 or 409": (r) => r.status === 201 || r.status === 409,
    });

    if (res.status === 201) {
      const body = JSON.parse(res.body);
      const instanceId = body.id;

      // Poll dashboard for status changes
      for (let i = 0; i < 60; i++) {
        sleep(5);
        const dashboard = getDashboardState(data.token);
        if (dashboard.status !== 200) continue;

        const state = JSON.parse(dashboard.body);
        if (state.instances && state.instances.length > 0) {
          const inst = state.instances[0];
          if (inst.status === "active" || inst.status === "error") break;
        }
      }

      // Clean up: delete the instance
      group("delete instance", () => {
        const delRes = deleteInstance(data.token, instanceId);
        check(delRes, {
          "delete returns 200": (r) => r.status === 200,
        });
      });
    } else if (!success) {
      provisioningErrors.add(1);
    }
  });

  sleep(10);
}

export function heartbeatAgent() {
  // Simulate agent heartbeat — uses a fake agent token
  // In a real test, you'd need valid agent tokens from seeded instances
  const res = healthz(); // Fallback: just hit health endpoint
  check(res, { "heartbeat proxy 200": (r) => r.status === 200 });
  sleep(300); // Every 5 minutes
}
