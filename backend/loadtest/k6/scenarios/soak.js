import { check, sleep } from "k6";
import { Trend, Counter } from "k6/metrics";
import { getDashboardState, healthz, getModels } from "../helpers.js";

const latencyTrend = new Trend("soak_latency");
const errorCount = new Counter("soak_errors");

export const options = {
  scenarios: {
    sustained: {
      executor: "constant-vus",
      vus: 30,
      duration: "4h",
    },
  },
  thresholds: {
    soak_latency: ["p(95)<500"],
    soak_errors: ["count<100"],
    http_req_failed: ["rate<0.005"],  // Very strict: < 0.5% over 4 hours
  },
};

export default function () {
  const vuId = `soak-${__VU}`;
  const rand = Math.random();

  let res;
  if (rand < 0.8) {
    // 80%: Dashboard polling
    res = getDashboardState(vuId);
  } else if (rand < 0.95) {
    // 15%: Health checks
    res = healthz();
  } else {
    // 5%: Models list
    res = getModels();
  }

  latencyTrend.add(res.timings.duration);

  const ok = check(res, {
    "response ok": (r) => r.status === 200,
    "latency under 1s": (r) => r.timings.duration < 1000,
  });

  if (!ok) {
    errorCount.add(1);
  }

  sleep(5);
}
