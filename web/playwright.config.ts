import { defineConfig } from '@playwright/test';

if (!process.env.E2E_BASE_URL)
  throw new Error('Run make e2e to use an isolated database and server.');

export default defineConfig({
  testDir: './e2e',
  workers: 1,
  retries: 0,
  timeout: 45_000,
  reporter: [['list'], ['json', { outputFile: 'test-results/results.json' }]],
  use: {
    baseURL: process.env.E2E_BASE_URL,
    channel: process.platform === 'darwin' ? 'chrome' : undefined,
    locale: 'ru-RU',
    timezoneId: 'Europe/Moscow',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [
    { name: 'desktop', use: { viewport: { width: 1440, height: 900 } } },
    {
      name: 'mobile',
      use: { viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true },
    },
  ],
});
