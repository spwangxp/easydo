import { test, expect } from '@playwright/test'

test('pipeline design reload keeps node positions from saved config', async ({ page }) => {
  await page.goto('http://127.0.0.1/login')

  const setupResult = await page.evaluate(async () => {
    const loginResponse = await fetch('/api/auth/login', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({
        username: 'demo',
        password: '1qaz2WSX'
      })
    })
    const loginPayload = await loginResponse.json()
    const token = loginPayload?.data?.token || ''
    if (!token) {
      return { token: '', projectStatus: 0, pipelineStatus: 0, updateStatus: 0, pipelineId: 0 }
    }

    const projectResponse = await fetch('/api/projects', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`
      },
      body: JSON.stringify({
        name: `Position Persist Project ${Date.now()}`,
        description: 'regression project',
        color: '#409EFF'
      })
    })
    const projectPayload = await projectResponse.json()
    const projectId = projectPayload?.data?.id || 0

    const baseConfig = {
      version: '2.0',
      nodes: [
        { id: 'node_1', type: 'shell', name: '开始任务', x: 120, y: 180, config: { script: 'echo start' } },
        { id: 'node_2', type: 'shell', name: '后续任务', x: 420, y: 260, config: { script: 'echo next' } }
      ],
      edges: [
        { from: 'node_1', to: 'node_2' }
      ]
    }

    const pipelineResponse = await fetch('/api/pipelines', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`
      },
      body: JSON.stringify({
        name: `Position Persist Pipeline ${Date.now()}`,
        description: 'regression pipeline',
        environment: 'test',
        project_id: projectId,
        config: JSON.stringify(baseConfig)
      })
    })
    const pipelinePayload = await pipelineResponse.json()
    const pipelineId = pipelinePayload?.data?.id || 0

    baseConfig.nodes[1].x = 760
    baseConfig.nodes[1].y = 440

    const updateResponse = await fetch(`/api/pipelines/${pipelineId}`, {
      method: 'PUT',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`
      },
      body: JSON.stringify({
        project_id: projectId,
        config: JSON.stringify(baseConfig)
      })
    })

    return {
      token,
      projectStatus: projectResponse.status,
      pipelineStatus: pipelineResponse.status,
      updateStatus: updateResponse.status,
      updateBody: await updateResponse.text(),
      pipelineId
    }
  })

  expect(setupResult.token).toBeTruthy()
  expect(setupResult.projectStatus).toBe(200)
  expect(setupResult.pipelineStatus).toBe(200)
  expect(setupResult.updateStatus, setupResult.updateBody).toBe(200)
  expect(setupResult.pipelineId).toBeTruthy()

  await page.addInitScript(({ token }) => {
    localStorage.setItem('token', token)
    localStorage.setItem('token_expires_at', String(Math.floor(Date.now() / 1000) + 3600))
    localStorage.setItem('token_refresh_interval', '600')
  }, { token: setupResult.token })

  await page.goto(`http://127.0.0.1/pipeline/${setupResult.pipelineId}`)
  await page.waitForSelector('.pipeline-design-container')
  await page.waitForSelector('.pipeline-node')

  const readNodePosition = async () => page.locator('.pipeline-node', { hasText: '后续任务' }).first().evaluate((node) => ({
    left: parseFloat(node.style.left || '0'),
    top: parseFloat(node.style.top || '0')
  }))

  const firstLoad = await readNodePosition()
  expect(firstLoad.left).toBe(760)
  expect(firstLoad.top).toBe(440)

  await page.reload()
  await page.waitForSelector('.pipeline-design-container')
  await page.waitForSelector('.pipeline-node')

  const secondLoad = await readNodePosition()
  expect(secondLoad.left).toBe(760)
  expect(secondLoad.top).toBe(440)
})
