// @ts-check
import { defineConfig, devices } from '@playwright/test'

// NOTE: Docker Compose must be running before executing tests:
//   docker compose -f ../docker-compose.yml up --build -d
// The frontend will be available at http://localhost:3000.

export default defineConfig({
  testDir: './e2e',
  timeout: 30000,
  retries: 0,
  fullyParallel: false,
  workers: 1,
  reporter: 'list',

  use: {
    baseURL: 'http://localhost:3000',
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
