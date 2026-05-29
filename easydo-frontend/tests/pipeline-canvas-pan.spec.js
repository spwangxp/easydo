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

test('pipeline canvas keeps fixed size cards and pans instead of zooming', async ({ page, request }) => {
  const authToken = await loginAsDemo(request)

  await page.addInitScript(({ token }) => {
    window.localStorage.setItem('token', token)
    window.localStorage.setItem('token_expires_at', String(Math.floor(Date.now() / 1000) + 3600))
    window.localStorage.setItem('token_refresh_interval', '600')
  }, { token: authToken })

  await page.goto('http://127.0.0.1/pipeline/1')
  await page.waitForSelector('.pipeline-design-container')
  await page.waitForSelector('.pipeline-node')

  await expect(page.locator('.toolbar-center')).toHaveCount(0)

  const canvas = page.locator('.canvas-wrapper')
  const firstNode = page.locator('.pipeline-node').first()

  const before = await page.evaluate(() => {
    const nodesLayer = document.querySelector('.nodes-layer')
    const firstNode = document.querySelector('.pipeline-node')
    return {
      transform: getComputedStyle(nodesLayer).transform,
      width: getComputedStyle(firstNode).width,
      height: getComputedStyle(firstNode).height
    }
  })

  await canvas.hover({ position: { x: 40, y: 40 } })
  await page.mouse.wheel(0, 800)
  await page.waitForTimeout(150)

  const afterWheel = await page.evaluate(() => {
    const nodesLayer = document.querySelector('.nodes-layer')
    const firstNode = document.querySelector('.pipeline-node')
    return {
      transform: getComputedStyle(nodesLayer).transform,
      width: getComputedStyle(firstNode).width,
      height: getComputedStyle(firstNode).height
    }
  })

  expect(afterWheel.width).toBe(before.width)
  expect(afterWheel.height).toBe(before.height)
  expect(afterWheel.transform).toBe(before.transform)

  const box = await canvas.boundingBox()
  expect(box).toBeTruthy()

  await page.mouse.move(box.x + 60, box.y + 60)
  await page.mouse.down()
  await page.mouse.move(box.x + 180, box.y + 140, { steps: 10 })
  await page.mouse.up()
  await page.waitForTimeout(150)

  const afterPan = await page.evaluate(() => {
    const nodesLayer = document.querySelector('.nodes-layer')
    return getComputedStyle(nodesLayer).transform
  })

  expect(afterPan).not.toBe(before.transform)
  await expect(firstNode).toBeVisible()
})
