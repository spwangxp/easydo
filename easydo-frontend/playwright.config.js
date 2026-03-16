import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './tests',
  timeout: 60000,
  use: {
    baseURL: process.env.EASYDO_BASE_URL || 'http://127.0.0.1',
    trace: 'retain-on-failure'
  }
})
