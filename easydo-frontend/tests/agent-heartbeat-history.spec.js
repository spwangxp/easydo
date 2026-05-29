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

test('执行器心跳记录可见', async ({ page }) => {
  await uiLogin(page)

  await page.goto('/agent')
  await page.waitForLoadState('networkidle')

  const firstRow = page.locator('.el-table__body-wrapper tbody tr').first()
  await expect(firstRow).toBeVisible({ timeout: 15000 })

  await firstRow.locator('.more-icon').click()
  await page.locator('.el-dropdown-menu__item:has-text("心跳记录")').first().click()

  const dialog = page.locator('.el-dialog:has-text("心跳记录")').last()
  await expect(dialog).toBeVisible({ timeout: 10000 })

  const dataRow = dialog.locator('.el-table__body-wrapper tbody tr')

  let found = false
  for (let i = 0; i < 8; i++) {
    if (await dataRow.count() > 0) {
      found = true
      break
    }
    await dialog.locator('button:has-text("刷新")').click()
    await page.waitForTimeout(1500)
  }

  expect(found).toBeTruthy()
})
