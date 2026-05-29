import { expect, test } from '@playwright/test'

const TEST_USER = {
  username: 'demo',
  password: '1qaz2WSX'
}

async function requestWithRetry(request, url, options = {}, attempts = 3) {
  let lastError = null
  for (let i = 0; i < attempts; i += 1) {
    try {
      const response = await request.fetch(url, options)
      if (response.ok()) {
        return response
      }
      lastError = new Error(`status=${response.status()}`)
    } catch (error) {
      lastError = error
    }
    await new Promise((resolve) => setTimeout(resolve, 1000))
  }
  throw lastError || new Error('request failed')
}

test('执行器心跳接口返回有效记录', async ({ request }) => {
  const loginResponse = await requestWithRetry(request, '/api/auth/login', {
    method: 'POST',
    data: TEST_USER,
    headers: {
      'Content-Type': 'application/json'
    }
  })
  const loginResult = await loginResponse.json()
  const token = loginResult?.data?.token
  expect(token).toBeTruthy()

  const authHeaders = { Authorization: `Bearer ${token}` }

  const agentsResponse = await requestWithRetry(request, '/api/agents', {
    method: 'GET',
    headers: authHeaders
  })
  const agentsResult = await agentsResponse.json()
  const agents = agentsResult?.data?.list || []
  expect(agents.length).toBeGreaterThan(0)

  const agentID = agents[0].id
  expect(agentID).toBeTruthy()

  const heartbeatResponse = await requestWithRetry(
    request,
    `/api/agents/${agentID}/heartbeats?page=1&page_size=20`,
    {
      method: 'GET',
      headers: authHeaders
    }
  )
  const heartbeatResult = await heartbeatResponse.json()
  const total = Number(heartbeatResult?.data?.total || 0)
  expect(total).toBeGreaterThan(0)

  const firstRecord = heartbeatResult?.data?.list?.[0]
  expect(firstRecord).toBeTruthy()
  expect(firstRecord).toHaveProperty('timestamp')
  expect(firstRecord).toHaveProperty('cpu_usage')
  expect(firstRecord).toHaveProperty('memory_usage')
  expect(firstRecord).toHaveProperty('disk_usage')
  expect(firstRecord).toHaveProperty('tasks_running')
})
