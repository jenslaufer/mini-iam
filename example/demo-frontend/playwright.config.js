// @ts-check
import { defineConfig, devices } from '@playwright/test'

// NOTE: The demo app and its backend must be running before executing tests.
// Typical setup via Docker Compose from the repo root:
//   docker compose -f docker-compose.yml up --build -d
// The demo frontend will be available at http://localhost:4000.

export default defineConfig({
  testDir: './e2e',
  timeout: 30000,
  retries: 0,
  fullyParallel: false,
  workers: 1,
  reporter: 'list',

  use: {
    baseURL: 'http://localhost:4000',
    headless: true,
    trace: 'on-first-retry',
  },

  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
})
