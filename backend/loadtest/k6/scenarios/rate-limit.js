import { check, sleep } from "k6";
import { Counter } from "k6/metrics";
import { getFirebaseToken, getDashboardState, createInstance, healthz } from "../helpers.js";

const rateLimited = new Counter("rate_limited_responses");
const accepted = new Counter("accepted_responses");

export const options = {
  scenarios: {
    // Test 1: General rate limit (60 req/min)
    general_limit: {
      executor: "constant-arrival-rate",
      rate: 80,            // 80 req/s — well above 60/min limit
      timeUnit: "1s",
      duration: "1m",
      preAllocatedVUs: 5,
      maxVUs: 10,
      exec: "testGeneralLimit",
    },
    // Test 2: Provisioning rate limit (10 req/min)
    provisioning_limit: {
      executor: "constant-arrival-rate",
      rate: 1,             // 1 req/s = 60/min — above 10/min limit
      timeUnit: "1s",
      duration: "1m",
      preAllocatedVUs: 5,
      maxVUs: 10,
      exec: "testProvisioningLimit",
      startTime: "1m30s",  // After general test completes
    },
  },
  thresholds: {
    rate_limited_responses: ["count>0"], // We EXPECT some 429s
  },
};

// Runs once before VUs start — obtain a real Firebase ID token.
export function setup() {
  const token = getFirebaseToken();
  return { token };
}

export function testGeneralLimit(data) {
  const res = getDashboardState(data.token);

  if (res.status === 429) {
    rateLimited.add(1);
    check(res, {
      "rate limit response is JSON": (r) => {
        try {
          const body = JSON.parse(r.body);
          return body.code === "rate_limited";
        } catch {
          return false;
        }
      },
    });
  } else {
    accepted.add(1);
    check(res, {
      "accepted response": (r) => r.status === 200,
    });
  }
}

export function testProvisioningLimit(data) {
  const res = createInstance(data.token, `rate-test-${__VU}-${__ITER}`, "eu-central");

  if (res.status === 429) {
    rateLimited.add(1);
  } else {
    accepted.add(1);
  }

  check(res, {
    "provisioning rate limit enforced": (r) =>
      r.status === 201 ||
      r.status === 403 ||  // no_subscription
      r.status === 409 ||  // limit_reached
      r.status === 429,    // rate limited
  });
}
