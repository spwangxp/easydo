import { test, expect } from '@playwright/test'

const TEST_USER = {
  username: 'demo',
  password: '1qaz2WSX'
}

test('refresh 接口续期时 token 保持不变', async ({ request }) => {
  const loginResponse = await request.post('/api/auth/login', {
    data: TEST_USER
  })
  expect(loginResponse.ok()).toBeTruthy()

  const loginResult = await loginResponse.json()
  const token = loginResult?.data?.token
  const expiresAt = Number(loginResult?.data?.expires_at || 0)
  expect(token).toBeTruthy()
  expect(expiresAt).toBeGreaterThan(0)

  const refreshResponse = await request.post('/api/auth/refresh', {
    headers: {
      Authorization: `Bearer ${token}`
    }
  })
  expect(refreshResponse.ok()).toBeTruthy()

  const refreshResult = await refreshResponse.json()
  expect(refreshResult?.data?.token).toBe(token)
  expect(Number(refreshResult?.data?.expires_at || 0)).toBeGreaterThanOrEqual(expiresAt)
})

test('token 无效时页面跳转到登录页', async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('token', 'invalid-token')
  })

  await page.goto('/')
  await page.waitForURL((url) => url.pathname.includes('/login'), { timeout: 15000 })
  await expect(page).toHaveURL(/\/login/)
})
