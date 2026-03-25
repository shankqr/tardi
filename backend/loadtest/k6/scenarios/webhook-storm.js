import { check, sleep } from "k6";
import { Counter } from "k6/metrics";
import { BASE_URL } from "../config.js";
import http from "k6/http";

const webhookSuccess = new Counter("webhook_success");
const webhookFailed = new Counter("webhook_failed");

export const options = {
  scenarios: {
    storm: {
      executor: "constant-vus",
      vus: 20,
      duration: "2m",
    },
  },
  thresholds: {
    http_req_duration: ["p(95)<1000"],
    // Webhooks will fail with 400 (invalid signature) in non-test mode
    // That's expected — we're testing the server doesn't crash under load
  },
};

export default function () {
  // Simulate a Stripe webhook event (will fail signature verification
  // unless STRIPE_WEBHOOK_SECRET is set to a known test value).
  // The point is to stress-test the webhook handler's throughput.
  const event = {
    id: `evt_loadtest_${__VU}_${__ITER}`,
    type: "customer.subscription.updated",
    data: {
      object: {
        id: `sub_loadtest_${__VU}`,
        status: "active",
        cancel_at_period_end: false,
        items: {
          data: [
            {
              current_period_end: Math.floor(Date.now() / 1000) + 86400 * 30,
              price: {
                metadata: { plan_tier: "standard" },
              },
            },
          ],
        },
      },
    },
  };

  const res = http.post(`${BASE_URL}/api/webhooks/stripe`, JSON.stringify(event), {
    headers: {
      "Content-Type": "application/json",
      "Stripe-Signature": "t=1234567890,v1=fake_signature_for_load_testing",
    },
  });

  const ok = check(res, {
    // 200 = processed, 400 = invalid signature (expected without test secret)
    "webhook handled": (r) => r.status === 200 || r.status === 400,
    "no server error": (r) => r.status < 500,
  });

  if (res.status === 200) {
    webhookSuccess.add(1);
  } else {
    webhookFailed.add(1);
  }

  sleep(0.1); // Rapid-fire
}
