import { expect, test } from '@playwright/test'

test('terminal page keeps hidden sessions dormant until selected', async ({ page }) => {
  const sessionStore = {
    'session-listed': {
      id: 11,
      session_id: 'session-listed',
      resource_id: 7,
      resource_type: 'vm',
      endpoint: '10.0.0.7:22',
      status: 'active',
      created_at: '2026-03-22T08:00:00Z',
      updated_at: '2026-03-22T08:00:00Z'
    }
  }
  const createCounters = { 7: 0, 8: 0 }

  await page.addInitScript(() => {
    localStorage.setItem('token', 'playwright-token')
    localStorage.setItem('current_workspace_id', '1')

    window.__mockTerminalSockets = []

    window.WebSocket = class MockWebSocket {
      constructor(url) {
        this.url = url
        this.readyState = 1
        this.sent = []
        window.__mockTerminalSockets.push(this)
        const parsedUrl = new URL(url)
        const sessionId = parsedUrl.searchParams.get('session_id')

        setTimeout(() => {
          this.onopen?.()
          this.onmessage?.({ data: JSON.stringify({ type: 'terminal_ready', payload: { session_id: sessionId } }) })
          this.onmessage?.({ data: JSON.stringify({ type: 'terminal_output', payload: { session_id: sessionId, data: `connected:${sessionId}\r\n` } }) })
        }, 0)
      }

      send(payload) {
        this.sent.push(JSON.parse(payload))
      }

      close(code = 1000, reason = 'client_disconnect') {
        this.readyState = 3
        this.onclose?.({ code, reason })
      }
    }
  })

  await page.route('**/api/auth/userinfo', async route => {
    await route.fulfill({
      json: {
        code: 200,
        data: {
          id: 1,
          username: 'admin',
          permissions: ['resource.read', 'resource.operate'],
          current_workspace: {
            id: 1,
            name: 'Workspace A',
            capabilities: ['resource.read', 'resource.operate']
          },
          workspaces: [
            {
              id: 1,
              name: 'Workspace A',
              capabilities: ['resource.read', 'resource.operate']
            }
          ]
        }
      }
    })
  })

  await page.route('**/api/resources', async route => {
    await route.fulfill({
      json: {
        code: 200,
        data: [
          { id: 7, name: 'alpha-vm', type: 'vm', endpoint: '10.0.0.7:22', status: 'online' },
          { id: 8, name: 'beta-vm', type: 'vm', endpoint: '10.0.0.8:22', status: 'online' }
        ]
      }
    })
  })

  await page.route('**/api/resources/*/terminal-sessions', async route => {
    const url = new URL(route.request().url())
    const pathParts = url.pathname.split('/')
    const resourceId = Number(pathParts[pathParts.length - 2])
    const method = route.request().method()

    if (method === 'GET') {
      const listed = Object.values(sessionStore).filter(item => item.resource_id === resourceId && item.status === 'active')
      await route.fulfill({ json: { code: 200, data: listed } })
      return
    }

    createCounters[resourceId] = (createCounters[resourceId] || 0) + 1
    const createdSessionId = `session-${resourceId}-${createCounters[resourceId]}`
    sessionStore[createdSessionId] = {
      id: 100 + createCounters[resourceId],
      session_id: createdSessionId,
      resource_id: resourceId,
      resource_type: 'vm',
      endpoint: resourceId === 7 ? '10.0.0.7:22' : '10.0.0.8:22',
      status: 'active',
      created_at: `2026-03-22T08:0${createCounters[resourceId] + 1}:00Z`,
      updated_at: `2026-03-22T08:0${createCounters[resourceId] + 1}:00Z`
    }
    await route.fulfill({ json: { code: 200, data: sessionStore[createdSessionId] } })
  })

  await page.route('**/api/resources/*/terminal-sessions/*', async route => {
    const url = new URL(route.request().url())
    const pathParts = url.pathname.split('/')
    const sessionId = pathParts[pathParts.length - 1]
    await route.fulfill({ json: { code: 200, data: sessionStore[sessionId] } })
  })

  await page.goto('/terminal?resourceId=7')

  await expect(page.getByText('打开会话')).toBeVisible()
  await expect(page.getByText('VM 资源列表')).toBeVisible()
  await expect(page.getByTestId('terminal-session-session-listed')).toBeVisible()
  await expect(page.getByTestId('terminal-session-session-7-1')).toBeVisible()
  await expect.poll(async () => page.evaluate(() => window.__mockTerminalSockets.length)).toBe(1)

  await page.getByTestId('terminal-session-session-listed').click()
  await expect.poll(async () => page.evaluate(() => window.__mockTerminalSockets.length)).toBe(2)

  await page.getByTestId('terminal-resource-7').click()
  await expect(page.getByTestId('terminal-session-session-7-2')).toBeVisible()
  await expect.poll(async () => page.evaluate(() => window.__mockTerminalSockets.length)).toBe(3)
})

test('terminal page normalizes VM resource types in sidebar list', async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('token', 'playwright-token')
    localStorage.setItem('current_workspace_id', '1')

    window.WebSocket = class MockWebSocket {
      constructor(url) {
        this.url = url
        this.readyState = 1
        const parsedUrl = new URL(url)
        const sessionId = parsedUrl.searchParams.get('session_id')
        setTimeout(() => {
          this.onopen?.()
          this.onmessage?.({ data: JSON.stringify({ type: 'terminal_ready', payload: { session_id: sessionId } }) })
        }, 0)
      }

      send() {}

      close(code = 1000, reason = 'client_disconnect') {
        this.readyState = 3
        this.onclose?.({ code, reason })
      }
    }
  })

  await page.route('**/api/auth/userinfo', async route => {
    await route.fulfill({
      json: {
        code: 200,
        data: {
          id: 1,
          username: 'admin',
          permissions: ['resource.read', 'resource.operate'],
          current_workspace: {
            id: 1,
            name: 'Workspace A',
            capabilities: ['resource.read', 'resource.operate']
          },
          workspaces: [
            {
              id: 1,
              name: 'Workspace A',
              capabilities: ['resource.read', 'resource.operate']
            }
          ]
        }
      }
    })
  })

  await page.route('**/api/resources', async route => {
    await route.fulfill({
      json: {
        code: 200,
        data: [
          { id: 7, name: 'alpha-vm', type: ' VM ', endpoint: '10.0.0.7:22', status: 'online' }
        ]
      }
    })
  })

  await page.route('**/api/resources/*/terminal-sessions', async route => {
    const method = route.request().method()
    if (method === 'GET') {
      await route.fulfill({ json: { code: 200, data: [] } })
      return
    }
    await route.fulfill({
      json: {
        code: 200,
        data: {
          id: 101,
          session_id: 'session-7-1',
          resource_id: 7,
          resource_type: 'vm',
          endpoint: '10.0.0.7:22',
          status: 'active',
          created_at: '2026-03-22T08:01:00Z',
          updated_at: '2026-03-22T08:01:00Z'
        }
      }
    })
  })

  await page.route('**/api/resources/*/terminal-sessions/*', async route => {
    await route.fulfill({
      json: {
        code: 200,
        data: {
          id: 101,
          session_id: 'session-7-1',
          resource_id: 7,
          resource_type: 'vm',
          endpoint: '10.0.0.7:22',
          status: 'active',
          created_at: '2026-03-22T08:01:00Z',
          updated_at: '2026-03-22T08:01:00Z'
        }
      }
    })
  })

  await page.goto('/terminal?resourceId=7')

  await expect(page.getByTestId('terminal-resource-7')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'alpha-vm' })).toBeVisible()
})

test('terminal page keeps workspace height bounded and renders terminal output', async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('token', 'playwright-token')
    localStorage.setItem('current_workspace_id', '1')

    window.__mockTerminalSockets = []

    window.WebSocket = class MockWebSocket {
      constructor(url) {
        this.url = url
        this.readyState = 1
        this.sent = []
        window.__mockTerminalSockets.push(this)
        const parsedUrl = new URL(url)
        const sessionId = parsedUrl.searchParams.get('session_id')

        setTimeout(() => {
          this.onopen?.()
          this.onmessage?.({ data: JSON.stringify({ type: 'terminal_ready', payload: { session_id: sessionId } }) })
          this.onmessage?.({ data: JSON.stringify({ type: 'terminal_output', payload: { session_id: sessionId, data: 'EASYDO_OK\\r\\n/data\\r\\n' } }) })
        }, 0)
      }

      send(payload) {
        this.sent.push(JSON.parse(payload))
      }

      close(code = 1000, reason = 'client_disconnect') {
        this.readyState = 3
        this.onclose?.({ code, reason })
      }
    }
  })

  await page.route('**/api/auth/userinfo', async route => {
    await route.fulfill({
      json: {
        code: 200,
        data: {
          id: 1,
          username: 'admin',
          permissions: ['resource.read', 'resource.operate'],
          current_workspace: {
            id: 1,
            name: 'Workspace A',
            capabilities: ['resource.read', 'resource.operate']
          },
          workspaces: [
            {
              id: 1,
              name: 'Workspace A',
              capabilities: ['resource.read', 'resource.operate']
            }
          ]
        }
      }
    })
  })

  await page.route('**/api/resources', async route => {
    await route.fulfill({
      json: {
        code: 200,
        data: [
          { id: 7, name: 'alpha-vm', type: 'vm', endpoint: '10.0.0.7:22', status: 'online' }
        ]
      }
    })
  })

  await page.route('**/api/resources/*/terminal-sessions', async route => {
    const method = route.request().method()
    if (method === 'GET') {
      await route.fulfill({ json: { code: 200, data: [] } })
      return
    }

    await route.fulfill({
      json: {
        code: 200,
        data: {
          id: 101,
          session_id: 'session-7-1',
          resource_id: 7,
          resource_type: 'vm',
          endpoint: '10.0.0.7:22',
          status: 'active',
          created_at: '2026-03-22T08:01:00Z',
          updated_at: '2026-03-22T08:01:00Z'
        }
      }
    })
  })

  await page.route('**/api/resources/*/terminal-sessions/*', async route => {
    await route.fulfill({
      json: {
        code: 200,
        data: {
          id: 101,
          session_id: 'session-7-1',
          resource_id: 7,
          resource_type: 'vm',
          endpoint: '10.0.0.7:22',
          status: 'active',
          created_at: '2026-03-22T08:01:00Z',
          updated_at: '2026-03-22T08:01:00Z'
        }
      }
    })
  })

  await page.goto('/terminal?resourceId=7')

  await expect.poll(async () => page.evaluate(() => window.__mockTerminalSockets.length)).toBe(1)

  await expect.poll(async () => {
    return page.evaluate(() => {
      const viewportHeight = window.innerHeight
      const pageHeight = document.querySelector('.terminal-page')?.getBoundingClientRect().height || 0
      const vmBlockTop = Array.from(document.querySelectorAll('h2')).find(el => el.textContent?.includes('VM 资源列表'))?.getBoundingClientRect().top || 0
      const hostHeight = document.querySelector('.terminal-host')?.getBoundingClientRect().height || 0
      const rowCount = document.querySelectorAll('.xterm-rows > div').length
      const terminalText = document.querySelector('.xterm')?.textContent || ''
      return {
        viewportHeight,
        pageHeight,
        vmBlockTop,
        hostHeight,
        rowCount,
        hasOutput: terminalText.includes('EASYDO_OK') && terminalText.includes('/data')
      }
    })
  }).toEqual(expect.objectContaining({
    hasOutput: true
  }))

  const metrics = await page.evaluate(() => {
    const viewportHeight = window.innerHeight
    const pageHeight = document.querySelector('.terminal-page')?.getBoundingClientRect().height || 0
    const vmBlockTop = Array.from(document.querySelectorAll('h2')).find(el => el.textContent?.includes('VM 资源列表'))?.getBoundingClientRect().top || 0
    const hostHeight = document.querySelector('.terminal-host')?.getBoundingClientRect().height || 0
    const rowCount = document.querySelectorAll('.xterm-rows > div').length
    return { viewportHeight, pageHeight, vmBlockTop, hostHeight, rowCount }
  })

  expect(metrics.pageHeight).toBeLessThan(metrics.viewportHeight * 3)
  expect(metrics.vmBlockTop).toBeLessThan(metrics.viewportHeight)
  expect(metrics.hostHeight).toBeLessThan(metrics.viewportHeight)
  expect(metrics.rowCount).toBeLessThan(500)
})

test('terminal page collapses and expands the sidebar', async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('token', 'playwright-token')
    localStorage.setItem('current_workspace_id', '1')

    window.WebSocket = class MockWebSocket {
      constructor(url) {
        this.url = url
        this.readyState = 1
        const parsedUrl = new URL(url)
        const sessionId = parsedUrl.searchParams.get('session_id')
        setTimeout(() => {
          this.onopen?.()
          this.onmessage?.({ data: JSON.stringify({ type: 'terminal_ready', payload: { session_id: sessionId } }) })
          this.onmessage?.({ data: JSON.stringify({ type: 'terminal_output', payload: { session_id: sessionId, data: 'connected\\r\\n' } }) })
        }, 0)
      }

      send() {}

      close(code = 1000, reason = 'client_disconnect') {
        this.readyState = 3
        this.onclose?.({ code, reason })
      }
    }
  })

  await page.route('**/api/auth/userinfo', async route => {
    await route.fulfill({
      json: {
        code: 200,
        data: {
          id: 1,
          username: 'admin',
          permissions: ['resource.read', 'resource.operate'],
          current_workspace: { id: 1, name: 'Workspace A', capabilities: ['resource.read', 'resource.operate'] },
          workspaces: [{ id: 1, name: 'Workspace A', capabilities: ['resource.read', 'resource.operate'] }]
        }
      }
    })
  })

  await page.route('**/api/resources', async route => {
    await route.fulfill({
      json: {
        code: 200,
        data: [{ id: 7, name: 'alpha-vm', type: 'vm', endpoint: '10.0.0.7:22', status: 'online' }]
      }
    })
  })

  await page.route('**/api/resources/*/terminal-sessions', async route => {
    const method = route.request().method()
    if (method === 'GET') {
      await route.fulfill({ json: { code: 200, data: [] } })
      return
    }
    await route.fulfill({
      json: {
        code: 200,
        data: {
          id: 101,
          session_id: 'session-7-1',
          resource_id: 7,
          resource_type: 'vm',
          endpoint: '10.0.0.7:22',
          status: 'active',
          created_at: '2026-03-22T08:01:00Z',
          updated_at: '2026-03-22T08:01:00Z'
        }
      }
    })
  })

  await page.route('**/api/resources/*/terminal-sessions/*', async route => {
    await route.fulfill({
      json: {
        code: 200,
        data: {
          id: 101,
          session_id: 'session-7-1',
          resource_id: 7,
          resource_type: 'vm',
          endpoint: '10.0.0.7:22',
          status: 'active',
          created_at: '2026-03-22T08:01:00Z',
          updated_at: '2026-03-22T08:01:00Z'
        }
      }
    })
  })

  await page.goto('/terminal?resourceId=7')

  await expect(page.getByText('打开会话')).toBeVisible()
  await expect(page.getByText('VM 资源列表')).toBeVisible()

  await page.getByRole('button', { name: '折叠侧栏' }).click()
  await expect(page.getByText('打开会话')).not.toBeVisible()
  await expect(page.getByText('VM 资源列表')).not.toBeVisible()
  await expect(page.getByRole('button', { name: '展开侧栏' })).toBeVisible()

  await page.getByRole('button', { name: '展开侧栏' }).click()
  await expect(page.getByText('打开会话')).toBeVisible()
  await expect(page.getByText('VM 资源列表')).toBeVisible()
})

test('terminal page sends root switch action for the active session', async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('token', 'playwright-token')
    localStorage.setItem('current_workspace_id', '1')

    window.__mockTerminalSockets = []

    window.WebSocket = class MockWebSocket {
      constructor(url) {
        this.url = url
        this.readyState = 1
        this.sent = []
        window.__mockTerminalSockets.push(this)
        const parsedUrl = new URL(url)
        const sessionId = parsedUrl.searchParams.get('session_id')
        setTimeout(() => {
          this.onopen?.()
          this.onmessage?.({ data: JSON.stringify({ type: 'terminal_ready', payload: { session_id: sessionId } }) })
        }, 0)
      }

      send(payload) {
        this.sent.push(JSON.parse(payload))
      }

      close(code = 1000, reason = 'client_disconnect') {
        this.readyState = 3
        this.onclose?.({ code, reason })
      }
    }
  })

  await page.route('**/api/auth/userinfo', async route => {
    await route.fulfill({
      json: {
        code: 200,
        data: {
          id: 1,
          username: 'admin',
          permissions: ['resource.read', 'resource.operate'],
          current_workspace: { id: 1, name: 'Workspace A', capabilities: ['resource.read', 'resource.operate'] },
          workspaces: [{ id: 1, name: 'Workspace A', capabilities: ['resource.read', 'resource.operate'] }]
        }
      }
    })
  })

  await page.route('**/api/resources', async route => {
    await route.fulfill({
      json: {
        code: 200,
        data: [{ id: 7, name: 'alpha-vm', type: 'vm', endpoint: '10.0.0.7:22', status: 'online' }]
      }
    })
  })

  await page.route('**/api/resources/*/terminal-sessions', async route => {
    const method = route.request().method()
    if (method === 'GET') {
      await route.fulfill({ json: { code: 200, data: [] } })
      return
    }
    await route.fulfill({
      json: {
        code: 200,
        data: {
          id: 101,
          session_id: 'session-7-1',
          resource_id: 7,
          resource_type: 'vm',
          endpoint: '10.0.0.7:22',
          status: 'active',
          created_at: '2026-03-22T08:01:00Z',
          updated_at: '2026-03-22T08:01:00Z'
        }
      }
    })
  })

  await page.route('**/api/resources/*/terminal-sessions/*', async route => {
    await route.fulfill({
      json: {
        code: 200,
        data: {
          id: 101,
          session_id: 'session-7-1',
          resource_id: 7,
          resource_type: 'vm',
          endpoint: '10.0.0.7:22',
          status: 'active',
          created_at: '2026-03-22T08:01:00Z',
          updated_at: '2026-03-22T08:01:00Z'
        }
      }
    })
  })

  await page.goto('/terminal?resourceId=7')
  await page.getByRole('button', { name: '切换 root' }).click()

  await expect.poll(async () => {
    return page.evaluate(() => window.__mockTerminalSockets[0]?.sent?.some(message => message.type === 'terminal_root_switch'))
  }).toBe(true)
})

test('terminal page reconnects after passive websocket close', async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('token', 'playwright-token')
    localStorage.setItem('current_workspace_id', '1')

    window.__mockTerminalSockets = []

    window.WebSocket = class MockWebSocket {
      constructor(url) {
        this.url = url
        this.readyState = 1
        this.sent = []
        window.__mockTerminalSockets.push(this)
        const parsedUrl = new URL(url)
        const sessionId = parsedUrl.searchParams.get('session_id')
        const socketIndex = window.__mockTerminalSockets.length

        setTimeout(() => {
          this.onopen?.()
          this.onmessage?.({ data: JSON.stringify({ type: 'terminal_ready', payload: { session_id: sessionId } }) })
          this.onmessage?.({ data: JSON.stringify({ type: 'terminal_output', payload: { session_id: sessionId, data: socketIndex === 1 ? 'first-connect\\r\\n' : 'reconnected\\r\\n' } }) })
        }, 0)
      }

      send(payload) {
        this.sent.push(JSON.parse(payload))
      }

      close(code = 1000, reason = 'client_disconnect') {
        this.readyState = 3
        this.onclose?.({ code, reason })
      }
    }
  })

  await page.route('**/api/auth/userinfo', async route => {
    await route.fulfill({
      json: {
        code: 200,
        data: {
          id: 1,
          username: 'admin',
          permissions: ['resource.read', 'resource.operate'],
          current_workspace: { id: 1, name: 'Workspace A', capabilities: ['resource.read', 'resource.operate'] },
          workspaces: [{ id: 1, name: 'Workspace A', capabilities: ['resource.read', 'resource.operate'] }]
        }
      }
    })
  })

  await page.route('**/api/resources', async route => {
    await route.fulfill({
      json: {
        code: 200,
        data: [{ id: 7, name: 'alpha-vm', type: 'vm', endpoint: '10.0.0.7:22', status: 'online' }]
      }
    })
  })

  await page.route('**/api/resources/*/terminal-sessions', async route => {
    const method = route.request().method()
    if (method === 'GET') {
      await route.fulfill({ json: { code: 200, data: [] } })
      return
    }
    await route.fulfill({
      json: {
        code: 200,
        data: {
          id: 101,
          session_id: 'session-7-1',
          resource_id: 7,
          resource_type: 'vm',
          endpoint: '10.0.0.7:22',
          status: 'active',
          created_at: '2026-03-22T08:01:00Z',
          updated_at: '2026-03-22T08:01:00Z'
        }
      }
    })
  })

  await page.route('**/api/resources/*/terminal-sessions/*', async route => {
    await route.fulfill({
      json: {
        code: 200,
        data: {
          id: 101,
          session_id: 'session-7-1',
          resource_id: 7,
          resource_type: 'vm',
          endpoint: '10.0.0.7:22',
          status: 'active',
          created_at: '2026-03-22T08:01:00Z',
          updated_at: '2026-03-22T08:01:00Z'
        }
      }
    })
  })

  await page.goto('/terminal?resourceId=7')
  await expect.poll(async () => page.evaluate(() => window.__mockTerminalSockets.length)).toBe(1)

  await page.evaluate(() => {
    window.__mockTerminalSockets[0].close(4001, 'network_drop')
  })

  await expect.poll(async () => page.evaluate(() => window.__mockTerminalSockets.length)).toBe(2)
  await expect.poll(async () => page.locator('body').innerText()).toContain('reconnected')
})
