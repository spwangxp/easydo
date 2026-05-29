import { test, expect } from '@playwright/test'

test('pipeline design uses a refined hollow arrowhead outside the target node', async ({ page, request }) => {
  const loginResponse = await request.post('http://127.0.0.1/api/auth/login', {
    data: {
      username: 'demo',
      password: '1qaz2WSX'
    }
  })
  expect(loginResponse.ok()).toBeTruthy()

  const loginPayload = await loginResponse.json()
  const authToken = loginPayload?.data?.token
  expect(authToken).toBeTruthy()

  await page.addInitScript(({ token }) => {
    window.localStorage.setItem('token', token)
    window.localStorage.setItem('token_expires_at', String(Math.floor(Date.now() / 1000) + 3600))
    window.localStorage.setItem('token_refresh_interval', '600')
  }, { token: authToken })

  await page.goto('http://127.0.0.1/pipeline/1')
  await page.waitForSelector('.pipeline-design-container')
  await page.waitForSelector('.connection-line')

  const inspection = await page.evaluate(() => {
    const targetNode = Array.from(document.querySelectorAll('.pipeline-node')).find((element) =>
      element.textContent?.includes('后续任务')
    )
    const connection = document.querySelector('.connection-line')
    const marker = document.querySelector('#arrowhead')
    const markerShape = marker?.firstElementChild
    const path = connection?.getAttribute('d') || ''
    const markerEnd = connection?.getAttribute('marker-end') || ''
    const matches = path.match(/(-?\d+(?:\.\d+)?)/g) || []
    const endX = Number(matches.at(-2))
    const targetLeft = Number.parseFloat(targetNode?.style.left || 'NaN')
    const strokeWidth = Number.parseFloat(getComputedStyle(connection).strokeWidth || '0')

    return {
      markerEnd,
      endX,
      targetLeft,
      strokeWidth,
      markerTag: markerShape?.tagName?.toLowerCase() || '',
      markerFill: markerShape?.getAttribute('fill') || '',
      markerStrokeLinecap: markerShape?.getAttribute('stroke-linecap') || '',
      markerStrokeLinejoin: markerShape?.getAttribute('stroke-linejoin') || '',
      markerD: markerShape?.getAttribute('d') || '',
      targetName: targetNode?.textContent || ''
    }
  })

  expect(inspection.targetName).toContain('后续任务')
  expect(inspection.markerEnd).toContain('arrowhead')
  expect(Number.isFinite(inspection.endX)).toBeTruthy()
  expect(Number.isFinite(inspection.targetLeft)).toBeTruthy()
  expect(inspection.endX).toBeLessThan(inspection.targetLeft)

  expect(inspection.markerTag).toBe('path')
  expect(inspection.markerFill).toBe('none')
  expect(inspection.markerStrokeLinecap).toBe('round')
  expect(inspection.markerStrokeLinejoin).toBe('round')
  expect(inspection.markerD).toContain('L')
  expect(inspection.strokeWidth).toBeLessThan(2.7)
})
