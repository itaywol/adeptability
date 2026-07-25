// PostHog analytics, bundled from npm (no external script load).
// The key is injected at build time from PUBLIC_POSTHOG_KEY (site/.env
// locally, a GitHub Actions variable in CI). No key = analytics off.
import posthog from 'posthog-js';

const key = import.meta.env.PUBLIC_POSTHOG_KEY;
if (key) {
  posthog.init(key, {
    api_host: 'https://us.i.posthog.com',
    defaults: '2025-05-24',
  });
}
