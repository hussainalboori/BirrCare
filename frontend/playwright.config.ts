import { defineConfig, devices } from "@playwright/test";

/**
 * E2E RTL/LTR checks. All /api/v1 calls are mocked with page.route() inside the
 * specs, so no Go backend is required — only the Next.js dev server.
 *   npx playwright install chromium webkit   (one-time)
 *   npm run test:e2e
 */
export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  retries: process.env.CI ? 1 : 0,
  use: {
    baseURL: process.env.E2E_BASE_URL || "http://127.0.0.1:3100",
    trace: "retain-on-failure",
  },
  projects: [
    { name: "mobile-safari", use: { ...devices["iPhone 13"] } },
    { name: "mobile-chrome", use: { ...devices["Pixel 7"] } },
  ],
  webServer: process.env.E2E_BASE_URL
    ? undefined
    : {
        command: "npm run dev -- --hostname 127.0.0.1 --port 3100",
        url: "http://127.0.0.1:3100",
        reuseExistingServer: false,
        timeout: 120_000,
        // The household's emergency number, sent in every page as the offline fallback,
        // and the version the pages say they were loaded from.
        env: { EMERGENCY_NUMBER: "999", GIT_SHA: "aaaaaaa1111111111111111111111111111111" },
      },
});
