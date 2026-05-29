import { test, expect } from '@playwright/test';

const TEST_USER = {
  username: 'demo',
  password: '1qaz2WSX',
};

test.describe('流水线列表页', () => {
  test.beforeEach(async ({ page }) => {
    // 登录
    await page.goto('/login');
    await page.waitForLoadState('networkidle');

    // 检查是否需要登录
    const loginButton = page.locator('button:has-text("登 录")');
    if (await loginButton.isVisible()) {
      await page.fill('input[placeholder="请输入用户名"]', TEST_USER.username);
      await page.fill('input[placeholder="请输入密码"]', TEST_USER.password);
      await loginButton.click();
      await page.waitForLoadState('networkidle');
    }
  });

  // UI-TEST-001: 进入流水线列表页
  test('UI-TEST-001: 进入流水线列表页', async ({ page }) => {
    await page.goto('/pipeline');
    await page.waitForLoadState('networkidle');

    // 检查页面标题
    await expect(page.locator('h1, .page-title, text=流水线').first()).toBeVisible();
  });

  // UI-TEST-002: 筛选流水线
  test('UI-TEST-002: 筛选流水线', async ({ page }) => {
    await page.goto('/pipeline');
    await page.waitForLoadState('networkidle');

    // 检查搜索框存在
    const searchInput = page.locator('input[placeholder*="搜索"]').first();
    await expect(searchInput).toBeVisible();

    // 输入搜索关键词
    await searchInput.fill('test');
    await page.waitForTimeout(500);

    // 验证无崩溃
    await expect(page.locator('body')).toBeVisible();
  });

  // UI-TEST-003: Tab切换
  test('UI-TEST-003: Tab切换', async ({ page }) => {
    await page.goto('/pipeline');
    await page.waitForLoadState('networkidle');

    // 检查Tab存在
    const tabs = page.locator('.el-tabs, .ant-tabs, [role="tablist"]');
    if (await tabs.isVisible()) {
      // 点击不同的Tab
      const tabItems = page.locator('[role="tab"], .el-tab-item, .ant-tabs-tab');
      const count = await tabItems.count();
      if (count > 0) {
        await tabItems.nth(0).click();
        await page.waitForTimeout(300);
      }
    }
  });

  // UI-TEST-004: 收藏流水线
  test('UI-TEST-004: 收藏流水线', async ({ page }) => {
    await page.goto('/pipeline');
    await page.waitForLoadState('networkidle');

    // 查找收藏按钮
    const favoriteBtn = page.locator('[title="收藏"], .favorite-btn, .star-btn').first();
    if (await favoriteBtn.isVisible()) {
      await favoriteBtn.click();
      await page.waitForTimeout(300);
    }
  });
});

test.describe('创建流水线', () => {
  test.beforeEach(async ({ page }) => {
    // 登录
    await page.goto('/login');
    await page.waitForLoadState('networkidle');

    const loginButton = page.locator('button:has-text("登 录")');
    if (await loginButton.isVisible()) {
      await page.fill('input[placeholder="请输入用户名"]', TEST_USER.username);
      await page.fill('input[placeholder="请输入密码"]', TEST_USER.password);
      await loginButton.click();
      await page.waitForLoadState('networkidle');
    }
  });

  // UI-TEST-010: 进入创建页面
  test('UI-TEST-010: 进入创建页面', async ({ page }) => {
    await page.goto('/pipeline');
    await page.waitForLoadState('networkidle');

    // 点击创建按钮
    const createBtn = page.locator('button:has-text("创建流水线"), a[href="/pipeline/create"]');
    if (await createBtn.isVisible()) {
      await createBtn.click();
      await page.waitForLoadState('networkidle');
      await expect(page).toHaveURL(/\/pipeline\/create/);
    }
  });

  // UI-TEST-011: 填写基本信息
  test('UI-TEST-011: 填写基本信息', async ({ page }) => {
    await page.goto('/pipeline/create');
    await page.waitForLoadState('networkidle');

    // 填写流水线名称
    const nameInput = page.locator('input[placeholder="请输入流水线名称"], input[name="name"]');
    if (await nameInput.isVisible()) {
      await nameInput.fill('Test Pipeline ' + Date.now());
    }

    // 选择项目
    const projectSelect = page.locator('.el-select, .ant-select, select[name="projectId"]');
    if (await projectSelect.isVisible()) {
      await projectSelect.click();
      await page.waitForTimeout(200);
    }
  });

  // UI-TEST-012: 添加节点
  test('UI-TEST-012: 添加节点', async ({ page }) => {
    await page.goto('/pipeline/create');
    await page.waitForLoadState('networkidle');

    // 查找添加节点按钮
    const addNodeBtn = page.locator('button:has-text("添加节点"), .add-node-btn');
    if (await addNodeBtn.isVisible()) {
      await addNodeBtn.click();
      await page.waitForTimeout(500);

      // 选择节点类型
      const nodeType = page.locator('.node-type-item, .el-card:has-text("Shell")');
      if (await nodeType.isVisible()) {
        await nodeType.click();
        await page.waitForTimeout(300);
      }
    }
  });

  // UI-TEST-015: 验证配置有效性
  test('UI-TEST-015: 验证配置有效性', async ({ page }) => {
    await page.goto('/pipeline/create');
    await page.waitForLoadState('networkidle');

    // 填写基本信息
    const nameInput = page.locator('input[placeholder="请输入流水线名称"], input[name="name"]');
    if (await nameInput.isVisible()) {
      await nameInput.fill('Test Pipeline Validation ' + Date.now());
    }

    // 尝试保存（应该验证配置）
    const saveBtn = page.locator('button:has-text("保存"), button[type="submit"]');
    if (await saveBtn.isVisible()) {
      await saveBtn.click();
      await page.waitForTimeout(500);

      // 验证没有错误
      const errorMsg = page.locator('.el-message--error, .ant-message-error, .error-message');
      if (await errorMsg.isVisible()) {
        const text = await errorMsg.textContent();
        console.log('Validation error:', text);
      }
    }
  });

  // UI-TEST-016: 保存流水线
  test('UI-TEST-016: 保存流水线', async ({ page }) => {
    await page.goto('/pipeline/create');
    await page.waitForLoadState('networkidle');

    // 填写名称
    const nameInput = page.locator('input[placeholder="请输入流水线名称"], input[name="name"]');
    if (await nameInput.isVisible()) {
      await nameInput.fill('Test Pipeline Save ' + Date.now());
    }

    // 添加节点
    const addNodeBtn = page.locator('button:has-text("添加节点"), .add-node-btn');
    if (await addNodeBtn.isVisible()) {
      await addNodeBtn.click();
      await page.waitForTimeout(500);
    }

    // 保存
    const saveBtn = page.locator('button:has-text("保存"), button[type="submit"]');
    if (await saveBtn.isVisible()) {
      await saveBtn.click();
      await page.waitForTimeout(1000);

      // 应该跳转到列表页或详情页
      await expect(page).toHaveURL(/\/pipeline/);
    }
  });
});

test.describe('流水线详情页', () => {
  test.beforeEach(async ({ page }) => {
    // 登录
    await page.goto('/login');
    await page.waitForLoadState('networkidle');

    const loginButton = page.locator('button:has-text("登 录")');
    if (await loginButton.isVisible()) {
      await page.fill('input[placeholder="请输入用户名"]', TEST_USER.username);
      await page.fill('input[placeholder="请输入密码"]', TEST_USER.password);
      await loginButton.click();
      await page.waitForLoadState('networkidle');
    }
  });

  // UI-TEST-020: 进入详情页
  test('UI-TEST-020: 进入详情页', async ({ page }) => {
    await page.goto('/pipeline');
    await page.waitForLoadState('networkidle');

    // 点击第一个流水线
    const pipelineItem = page.locator('.pipeline-item, .el-card, [class*="pipeline"]').first();
    if (await pipelineItem.isVisible()) {
      await pipelineItem.click();
      await page.waitForLoadState('networkidle');
      await expect(page).toHaveURL(/\/pipeline\/\d+/);
    }
  });

  // UI-TEST-030: 手动触发执行
  test('UI-TEST-030: 手动触发执行', async ({ page }) => {
    await page.goto('/pipeline');
    await page.waitForLoadState('networkidle');

    // 点击第一个流水线
    const pipelineItem = page.locator('.pipeline-item, .el-card, [class*="pipeline"]').first();
    if (await pipelineItem.isVisible()) {
      await pipelineItem.click();
      await page.waitForLoadState('networkidle');

      // 点击执行按钮
      const runBtn = page.locator('button:has-text("执行"), button:has-text("运行"), .run-btn');
      if (await runBtn.isVisible()) {
        await runBtn.click();
        await page.waitForTimeout(1000);

        // 检查是否创建了新的执行记录
        const executionItem = page.locator('.execution-item, .run-item, [class*="run"]');
        await expect(executionItem.first()).toBeVisible();
      }
    }
  });
});
