import { check, sleep, group } from "k6";
import { Counter, Trend } from "k6/metrics";
import http from "k6/http";
import { DEV_THRESHOLDS, BASE_URL } from "../config.js";
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
    ...DEV_THRESHOLDS,
    dashboard_latency: ["p(95)<5000"],
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

export function heartbeatAgent(data) {
  // Simulate agent heartbeat — uses a fake agent token so expect 401.
  // This tests that the heartbeat endpoint stays responsive under load.
  const agentToken = `agent-loadtest-${__VU}`;
  const res = http.post(
    `${BASE_URL}/api/agent/heartbeat`,
    JSON.stringify({ status: "healthy", openclaw_version: "1.0.0-loadtest" }),
    {
      headers: {
        Authorization: `Bearer ${agentToken}`,
        "Content-Type": "application/json",
      },
    },
  );
  check(res, {
    "heartbeat responded": (r) => r.status === 200 || r.status === 401 || r.status === 404,
  });
  sleep(30); // Every 30s during load test (faster than prod's 5m)
}
