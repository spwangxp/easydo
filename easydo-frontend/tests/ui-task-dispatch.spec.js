import { test, expect } from '@playwright/test'

const TEST_USER = {
  username: 'demo',
  password: '1qaz2WSX'
}

async function uiLogin(page) {
  await page.goto('/login')
  await page.waitForLoadState('networkidle')

  const loginBtn = page.locator('button:has-text("登 录")')
  if (await loginBtn.isVisible()) {
    await page.locator('input[placeholder*="用户名"], input[placeholder*="邮箱"]').first().fill(TEST_USER.username)
    await page.locator('input[placeholder*="密码"]').first().fill(TEST_USER.password)
    await loginBtn.click()
    await page.waitForURL((url) => !url.pathname.includes('/login'), { timeout: 15000 })
  }
}

test.describe.serial('任务调度视图', () => {
  test('执行器管理与工作台展示任务调度信息', async ({ page }) => {
    await uiLogin(page)

    await page.goto('/agent')
    await page.waitForLoadState('networkidle')

    const firstRow = page.locator('.el-table__body-wrapper tbody tr').first()
    await expect(firstRow).toBeVisible({ timeout: 15000 })

    await firstRow.locator('.more-icon').click()
    await page.locator('.el-dropdown-menu__item:has-text("任务列表")').first().click()

    const taskDialog = page.locator('.el-dialog:has-text("任务列表 -")').last()
    await expect(taskDialog).toBeVisible({ timeout: 10000 })
    await expect(taskDialog.locator('.el-table')).toBeVisible()
    await expect(taskDialog.locator('th:has-text("任务状态")').first()).toBeVisible()
    await expect(taskDialog.locator('th:has-text("流水线状态")').first()).toBeVisible()

    await taskDialog.locator('button:has-text("关闭")').click()

    await page.goto('/dashboard')
    await page.waitForLoadState('networkidle')

    const dispatchCard = page.locator('.section-card:has-text("任务调度视图")').first()
    await expect(dispatchCard).toBeVisible({ timeout: 10000 })
    await expect(dispatchCard.locator('th:has-text("调度状态")').first()).toBeVisible()
    await expect(dispatchCard.locator('th:has-text("任务状态")').first()).toBeVisible()
  })
})
