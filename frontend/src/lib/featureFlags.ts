/**
 * Feature flags — lightweight frontend-only toggles.
 *
 * Each flag reads from a VITE_FF_* env var at build time.
 * Default is false when the env var is absent.
 *
 * To enable a flag, set it in .env or wrangler.toml:
 *   VITE_FF_MODEL_SELECTION=true
 */
export const featureFlags = {
	modelSelection: import.meta.env.VITE_FF_MODEL_SELECTION === 'true',
} as const;
