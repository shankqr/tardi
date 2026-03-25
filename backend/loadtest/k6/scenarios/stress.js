import { check, sleep } from "k6";
import { STRESS_THRESHOLDS } from "../config.js";
import { getFirebaseToken, getDashboardState, createInstance, healthz } from "../helpers.js";

export const options = {
  // Ramp up to find breaking point
  stages: [
    { duration: "2m", target: 10 },   // Below normal
    { duration: "3m", target: 50 },   // Normal
    { duration: "3m", target: 100 },  // Above normal
    { duration: "3m", target: 200 },  // Breaking point zone
    { duration: "2m", target: 50 },   // Recovery
    { duration: "2m", target: 0 },    // Ramp down
  ],
  thresholds: STRESS_THRESHOLDS,
};

// Runs once before VUs start — obtain a real Firebase ID token.
export function setup() {
  const token = getFirebaseToken();
  return { token };
}

export default function (data) {
  // Mix of operations weighted by real-world frequency
  const rand = Math.random();

  if (rand < 0.7) {
    // 70%: Dashboard polling (the dominant operation)
    const res = getDashboardState(data.token);
    check(res, { "dashboard ok": (r) => r.status === 200 });
  } else if (rand < 0.9) {
    // 20%: Health checks
    const res = healthz();
    check(res, { "healthz ok": (r) => r.status === 200 });
  } else {
    // 10%: Provisioning attempts
    const res = createInstance(data.token, `stress-agent-${__VU}`, "eu-central");
    check(res, {
      "create accepted": (r) =>
        r.status === 201 ||
        r.status === 409 ||  // limit_reached
        r.status === 403 ||  // no_subscription
        r.status === 429,    // rate limited
    });
  }

  sleep(Math.random() * 2 + 1); // 1-3s random delay
}
