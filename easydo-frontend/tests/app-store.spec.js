import { expect, test } from '@playwright/test'

const apiBase = process.env.EASYDO_API_BASE_URL || 'http://127.0.0.1:8080/api'

test('app store supports app detail, variant editing, and deploy dialog flows', async ({ page }) => {
  const session = await getAdminSession()
  const suffix = Date.now()
  const appName = `App Store E2E ${suffix}`

  const app = await apiRequest('/store/templates', {
    method: 'POST',
    token: session.token,
    workspaceId: session.workspaceId,
    data: {
      name: appName,
      category: 'cache',
      template_type: 'app',
      target_resource_type: 'vm',
      source: 'workspace',
      summary: '用于验证应用商店页面的测试应用',
      description: '覆盖卡片、详情、VM/K8s 编辑和部署弹窗的端到端检查'
    }
  })

  try {
    await apiRequest(`/store/templates/${app.data.id}/versions`, {
      method: 'POST',
      token: session.token,
      workspaceId: session.workspaceId,
      data: {
        version: '7.2.0-vm',
        status: 'published',
        infra_type: 'vm',
        version_description: 'VM stable variant',
        command_template: 'docker run -d --name {{container_name}} -p {{vm_port}}:{{redis_port}} redis:{{image_tag}}',
        parameters: [
          parameterRow('container_name', '容器名称', 'text', 'redis-e2e', 1),
          parameterRow('vm_port', '主机端口', 'number', '16379', 2),
          parameterRow('redis_port', '容器端口', 'number', '6379', 3),
          parameterRow('image_tag', '镜像版本', 'text', '7.2.0', 4, { advanced: true })
        ]
      }
    })

    await apiRequest(`/store/templates/${app.data.id}/versions`, {
      method: 'POST',
      token: session.token,
      workspaceId: session.workspaceId,
      data: {
        version: '19.6.0-k8s',
        status: 'published',
        infra_type: 'k8s',
        version_description: 'K8s stable variant',
        chart_source: {
          type: 'repo',
          repo_url: 'https://charts.bitnami.com/bitnami',
          chart_name: 'redis',
          chart_version: '19.6.0'
        },
        base_values_yaml: 'architecture: standalone\nauth:\n  enabled: true\n',
        parameters: [
          parameterRow('release_name', 'Release Name', 'text', 'redis-e2e', 1),
          parameterRow('namespace', 'Namespace', 'text', 'default', 2),
          parameterRow('image.tag', '镜像版本', 'text', '7.2.0', 3),
          parameterRow('primary.persistence.size', '存储大小', 'select', '8Gi', 4, {
            option_values: ['8Gi', '20Gi']
          })
        ]
      }
    })

    await page.addInitScript(({ token, workspaceId }) => {
      localStorage.setItem('token', token)
      localStorage.setItem('current_workspace_id', String(workspaceId))
    }, {
      token: session.token,
      workspaceId: session.workspaceId
    })

    await page.goto('/store/apps')
    await expect(page).toHaveURL(/\/store\/apps$/)
    await expect(page.locator('.store-header-card .page-title')).toHaveText('商店')
    await expect(page.getByRole('tab', { name: '应用商店' })).toBeVisible()
    await expect(page.getByRole('tab', { name: 'LLM 商店' })).toBeVisible()
    await page.getByRole('tab', { name: 'LLM 商店' }).click()
    await expect(page).toHaveURL(/\/store\/llms$/)
    await page.goto('/store/apps')
    await expect(page).toHaveURL(/\/store\/apps$/)

    await page.locator('.category-chip', { hasText: '缓存' }).click()
    const appCard = page.locator('.app-card').filter({ hasText: appName })
    await expect(appCard).toBeVisible()
    await expect(appCard).toContainText('工作空间')
    await expect(appCard).toContainText('缓存')
    await appCard.click()

    await expect(page.locator('.detail-header h2')).toHaveText(appName)
    await expect(page.locator('.variant-row')).toHaveCount(2)
    await expect(page.locator('.variant-row').filter({ hasText: 'VM stable variant' })).toContainText('VM')
    await expect(page.locator('.variant-row').filter({ hasText: 'K8s stable variant' })).toContainText('K8s')

    await page.getByRole('button', { name: '编辑应用' }).click()
    const appDialog = page.locator('.el-dialog').filter({ hasText: '编辑应用' })
    await expect(appDialog.locator('.el-form-item').filter({ hasText: '应用名称' }).locator('input')).toHaveValue(appName)
    await expect(appDialog.locator('.el-form-item').filter({ hasText: '一句话摘要' }).locator('input')).toHaveValue('用于验证应用商店页面的测试应用')
    await appDialog.getByRole('button', { name: '取消' }).click()

    const vmRow = page.locator('.variant-row').filter({ hasText: 'VM stable variant' })
    await vmRow.getByRole('button', { name: '编辑' }).click()
    const vmDialog = page.locator('.el-dialog').filter({ hasText: 'VM 命令模板' })
    await expect(vmDialog).toContainText('版本信息')
    await expect(vmDialog).toContainText('参数定义')
    await expect(vmDialog.locator('.mono-input textarea')).toHaveValue('docker run -d --name {{container_name}} -p {{vm_port}}:{{redis_port}} redis:{{image_tag}}')
    await expect(vmDialog.locator('.parameter-table .el-table__body tr')).toHaveCount(4)
    await vmDialog.getByRole('button', { name: '取消' }).click()

    const k8sRow = page.locator('.variant-row').filter({ hasText: 'K8s stable variant' })
    await k8sRow.getByRole('button', { name: '编辑' }).click()
    const k8sDialog = page.locator('.el-dialog').filter({ hasText: 'Chart Source' })
    await expect(k8sDialog).toContainText('Base values.yaml')
    await expect(k8sDialog.locator('.parameter-table .el-table__body tr')).toHaveCount(4)
    await expect(k8sDialog.locator('.el-form-item').filter({ hasText: 'Repo URL' }).locator('input')).toHaveValue('https://charts.bitnami.com/bitnami')
    await k8sDialog.getByRole('button', { name: '取消' }).click()

    await vmRow.getByRole('button', { name: '部署' }).click()
    const vmDeployDialog = page.locator('.el-dialog').filter({ hasText: '最终渲染后的命令' })
    await expect(vmDeployDialog).toContainText('目标资源')
    await expect(vmDeployDialog).toContainText('容器名称')
    await expect(vmDeployDialog).toContainText('主机端口')
    await expect(vmDeployDialog).toContainText('选择资源并填写参数后展示预览')
    await vmDeployDialog.getByRole('button', { name: '取消' }).click()

    await k8sRow.getByRole('button', { name: '部署' }).click()
    const k8sDeployDialog = page.locator('.el-dialog').filter({ hasText: '显示 Base + Diff 和最终 Helm 命令。' })
    await expect(k8sDeployDialog).toContainText('Release Name')
    await expect(k8sDeployDialog).toContainText('Namespace')
    await expect(k8sDeployDialog).toContainText('选择资源并填写参数后展示预览')
    await k8sDeployDialog.getByRole('button', { name: '取消' }).click()
  } finally {
    await deleteAppAndVersions(session, app.data.id).catch(() => {})
  }
})

function parameterRow(name, label, type, defaultValue, sortOrder, overrides = {}) {
  return {
    name,
    label,
    type,
    default_value: defaultValue,
    description: label,
    required: false,
    advanced: false,
    option_values: [],
    sort_order: sortOrder,
    ...overrides
  }
}

async function apiRequest(path, options = {}) {
  const method = options.method || 'GET'
  const body = options.data ? JSON.stringify(options.data) : undefined
  const response = await fetch(`${apiBase}${path}`, {
    method,
    headers: {
      'Content-Type': 'application/json',
      ...(options.token ? { Authorization: `Bearer ${options.token}` } : {}),
      ...(options.workspaceId ? { 'X-Workspace-ID': String(options.workspaceId) } : {})
    },
    body
  })
  const payload = await response.json()
  if (!response.ok) {
    throw new Error(payload?.message || `Request failed: ${response.status}`)
  }
  return payload
}

async function getAdminSession() {
  const login = await apiRequest('/auth/login', {
    method: 'POST',
    data: { username: 'admin', password: '1qaz2WSX' }
  })
  const token = login.data.token
  const userinfo = await apiRequest('/auth/userinfo', { token })
  const workspaceId = userinfo.data.workspaces[0].id
  return { token, workspaceId }
}

async function deleteAppAndVersions(session, templateId) {
  const versions = await apiRequest(`/store/templates/${templateId}/versions`, {
    token: session.token,
    workspaceId: session.workspaceId
  })

  for (const version of versions.data || []) {
    await apiRequest(`/store/templates/${templateId}/versions/${version.id}`, {
      method: 'DELETE',
      token: session.token,
      workspaceId: session.workspaceId
    })
  }

  await apiRequest(`/store/templates/${templateId}`, {
    method: 'DELETE',
    token: session.token,
    workspaceId: session.workspaceId
  })
}
