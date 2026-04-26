// Rewrites api.tardi.ai/* requests to the Cloud Run service hostname.
// Required because the migration plan opted for "Cloudflare-only path"
// (no google_cloud_run_domain_mapping) — Cloud Run rejects requests whose
// Host header doesn't match its own *.run.app domain or a mapped custom
// domain. This Worker overrides Host + URL host so Cloud Run accepts.
//
// To swap targets (e.g. region change, blue/green), update ORIGIN_HOST and
// `wrangler deploy`. The Worker route api.tardi.ai/* keeps DNS unchanged.

const ORIGIN_HOST = "tardi-api-prod-loy7nru5uq-uc.a.run.app";

export default {
  async fetch(request) {
    const url = new URL(request.url);
    url.hostname = ORIGIN_HOST;
    url.protocol = "https:";
    url.port = "";

    const headers = new Headers(request.headers);
    headers.set("Host", ORIGIN_HOST);

    return fetch(url.toString(), {
      method: request.method,
      headers,
      body: request.body,
      redirect: "manual",
    });
  },
};
