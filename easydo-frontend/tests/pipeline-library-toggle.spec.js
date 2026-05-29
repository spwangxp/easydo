import { test, expect } from '@playwright/test'

async function loginAsDemo(request) {
  const response = await request.post('http://127.0.0.1/api/auth/login', {
    data: {
      username: 'demo',
      password: '1qaz2WSX'
    }
  })
  expect(response.ok()).toBeTruthy()
  const payload = await response.json()
  expect(payload?.data?.token).toBeTruthy()
  return payload.data.token
}

test('component library defaults expanded and can be collapsed then expanded from external toggle', async ({ page, request }) => {
  const authToken = await loginAsDemo(request)

  await page.addInitScript(({ token }) => {
    window.localStorage.setItem('token', token)
    window.localStorage.setItem('token_expires_at', String(Math.floor(Date.now() / 1000) + 3600))
    window.localStorage.setItem('token_refresh_interval', '600')
  }, { token: authToken })

  await page.goto('http://127.0.0.1/pipeline/1')
  await page.waitForSelector('.pipeline-design-container')
  await page.waitForSelector('.components-panel .panel-header')

  const toggle = page.locator('.library-toggle-btn')
  const panel = page.locator('.components-panel')

  await expect(panel).not.toHaveClass(/collapsed/)
  await expect(page.locator('.components-panel .panel-header')).toContainText('组件库')
  await expect(page.locator('.components-panel .component-item').first()).toBeVisible()
  await expect(toggle).toHaveAttribute('aria-label', '折叠组件库')

  await toggle.click()
  await expect(panel).toHaveClass(/collapsed/)
  await expect(toggle).toBeVisible()
  await expect(toggle).toHaveAttribute('aria-label', '展开组件库')

  await page.waitForFunction(() => {
    const panel = document.querySelector('.components-panel')
    return panel && parseFloat(getComputedStyle(panel).width) < 5
  })

  const collapsedState = await page.evaluate(() => {
    const panel = document.querySelector('.components-panel')
    const width = getComputedStyle(panel).width
    return { width }
  })
  expect(parseFloat(collapsedState.width)).toBeLessThan(5)

  await toggle.click()
  await expect(panel).not.toHaveClass(/collapsed/)
  await expect(toggle).toHaveAttribute('aria-label', '折叠组件库')
  await page.waitForFunction(() => {
    const panel = document.querySelector('.components-panel')
    return panel && parseFloat(getComputedStyle(panel).width) > 250
  })

  const expandedState = await page.evaluate(() => {
    const panel = document.querySelector('.components-panel')
    const width = parseFloat(getComputedStyle(panel).width)
    const firstItem = document.querySelector('.components-panel .component-item')
    return {
      width,
      firstItemVisible: Boolean(firstItem && getComputedStyle(firstItem).display !== 'none')
    }
  })

  expect(expandedState.width).toBeGreaterThan(250)
  expect(expandedState.firstItemVisible).toBeTruthy()
})
