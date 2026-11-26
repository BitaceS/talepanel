import { defineConfig, devices } from '@playwright/test'

// End-to-end tests against a running demo stack (examples/demo).
//
//   docker compose -f examples/demo/compose.yml up -d --build   # or examples/demo/run-demo.sh
//   cd apps/web && npx playwright test
//
// E2E_BASE_URL points at another panel; the tests expect the demo seed
// (admin@demo.local and the three demo servers).
export default defineConfig({
  testDir: './e2e',
  // The tests share one demo stack and change server state (start/stop,
  // files), so they run one at a time.
  workers: 1,
  fullyParallel: false,
  retries: process.env.CI ? 1 : 0,
  timeout: 60_000,
  expect: { timeout: 15_000 },
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: process.env.E2E_BASE_URL ?? 'http://localhost:14000',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [
    { name: 'desktop', use: { ...devices['Desktop Chrome'] }, testIgnore: /mobile\.spec/ },
    { name: 'mobile', use: { ...devices['Desktop Chrome'], viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true }, testMatch: /mobile\.spec/ },
  ],
})
