import { defineConfig } from '@playwright/test';

export const baseURL = process.env.CZ_A11Y_BASE_URL || 'http://127.0.0.1:18631';

const desktop = { viewport: { width: 1440, height: 900 } };

export default defineConfig({
  testDir: '.',
  globalSetup: './global-setup.js',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  workers: process.env.CI ? 4 : 6,
  reporter: process.env.CI ? [['github'], ['list']] : 'list',
  use: {
    baseURL,
    storageState: '.cache/admin.json',
    trace: 'off',
  },
  projects: [
    { name: 'light', use: { ...desktop, colorScheme: 'light' } },
    { name: 'dark', use: { ...desktop, colorScheme: 'dark' }, testIgnore: /keyboard/ },
  ],
});
