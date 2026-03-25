// Shared configuration for all k6 load tests

export const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";

// Firebase auth config — pass via env vars or use defaults for dev
export const FIREBASE_API_KEY = __ENV.FIREBASE_API_KEY || "";
export const FIREBASE_EMAIL = __ENV.FIREBASE_EMAIL || "";
export const FIREBASE_PASSWORD = __ENV.FIREBASE_PASSWORD || "";

// Default thresholds applied to all scenarios
export const DEFAULT_THRESHOLDS = {
  http_req_duration: ["p(95)<500", "p(99)<1000"],
  http_req_failed: ["rate<0.01"],
};

// Stricter thresholds for smoke tests
export const SMOKE_THRESHOLDS = {
  http_req_duration: ["p(95)<200", "p(99)<500"],
  http_req_failed: ["rate<0.001"],
};

// Relaxed thresholds for stress tests (we expect some degradation)
export const STRESS_THRESHOLDS = {
  http_req_duration: ["p(95)<2000"],
  http_req_failed: ["rate<0.05"],
};
