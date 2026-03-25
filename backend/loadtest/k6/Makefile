BASE_URL ?= http://localhost:8080

# Smoke: verify all endpoints work (30s, 1-2 VUs)
smoke:
	k6 run --env BASE_URL=$(BASE_URL) scenarios/smoke.js

# Load: simulate normal production traffic (10min, ~65 VUs)
load:
	k6 run --env BASE_URL=$(BASE_URL) scenarios/load.js

# Stress: ramp to 200 VUs, find breaking point (15min)
stress:
	k6 run --env BASE_URL=$(BASE_URL) scenarios/stress.js

# Spike: sudden 5→100 VU burst, test recovery (5min)
spike:
	k6 run --env BASE_URL=$(BASE_URL) --env K6_START_TIME=$(shell date +%s)000 scenarios/spike.js

# Dashboard poll: 10→500 VUs, find pool saturation inflection (5min)
dashboard:
	k6 run --env BASE_URL=$(BASE_URL) scenarios/dashboard-poll.js

# Heartbeat flood: 200 concurrent agent heartbeats (2min)
heartbeat:
	k6 run --env BASE_URL=$(BASE_URL) scenarios/heartbeat-flood.js

# Webhook storm: rapid Stripe events (2min)
webhook:
	k6 run --env BASE_URL=$(BASE_URL) scenarios/webhook-storm.js

# Rate limit: verify 429 enforcement (2.5min)
ratelimit:
	k6 run --env BASE_URL=$(BASE_URL) scenarios/rate-limit.js

# Soak: 4-hour sustained load, detect leaks
soak:
	k6 run --env BASE_URL=$(BASE_URL) scenarios/soak.js

# Run against deployed dev environment
dev-%:
	$(MAKE) $* BASE_URL=https://tardi-api-dev-$(shell gcloud run services describe tardi-api-dev --region=us-central1 --format='value(status.url)' 2>/dev/null || echo "SET_DEV_URL")

# All quick tests (excludes soak)
all: smoke load stress spike dashboard heartbeat webhook ratelimit

.PHONY: smoke load stress spike dashboard heartbeat webhook ratelimit soak all
