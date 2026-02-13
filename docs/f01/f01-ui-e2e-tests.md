# F01 前端UI端到端测试用例

## 概述

本文档补充了前端UI层的端到端（E2E）测试用例，使用Playwright模拟真实用户在浏览器中的操作。

**测试工具**: Playwright  
**测试范围**: 流水线全生命周期的UI交互  
**用例数量**: 待统计

---

## 一、测试环境配置

### 1.1 测试环境要求

```yaml
测试环境:
  前端URL: http://localhost:5173
  后端URL: http://localhost:8080
  数据库: MySQL (Docker)
  Redis: Redis (Docker)
  Agent: 至少1个在线Agent

测试用户:
  admin: 1qaz2WSX (管理员)
  demo: 1qaz2WSX (管理员)
  test: 1qaz2WSX (普通用户)
```

### 1.2 Playwright配置

```javascript
// playwright.config.js
import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  timeout: 60000,  // 1分钟超时
  use: {
    baseURL: 'http://localhost:5173',
    headless: true,
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
  },
  projects: [
    {
      name: 'Chrome',
      use: { browserName: 'chromium' },
    },
  ],
});
```

---

## 二、流水线列表页测试

### UI-TEST-001: 进入流水线列表页（必过）
**用例ID**: UI-TEST-001  
**优先级**: P0  
**页面**: `/pipeline`

**前置条件**: 用户已登录

**操作步骤**:
```javascript
// 1. 导航到流水线列表页
await page.goto('/pipeline');

// 2. 等待页面加载
await page.waitForLoadState('networkidle');

// 3. 检查页面元素
```

**预期结果**:
| 元素 | 验证点 |
|------|--------|
| 页面标题 | 显示"流水线" |
| 创建按钮 | 可见且可点击 |
| 搜索框 | 可见 |
| 流水线列表 | 可见 |
| Tab切换 | 全部/我创建的/我收藏的 |

**验证方法**:
```javascript
// 检查标题
await expect(page.locator('h1')).toContainText('流水线');

// 检查创建按钮
await expect(page.locator('button:has-text("创建流水线")')).toBeVisible();

// 检查Tab
await expect(page.locator('.el-tabs >> text=全部')).toBeVisible();
await expect(page.locator('.el-tabs >> text=我创建的')).toBeVisible();
await expect(page.locator('.el-tabs >> text=我收藏的')).toBeVisible();

// 检查流水线列表
await expect(page.locator('.pipeline-list')).toBeVisible();
```

---

### UI-TEST-002: 筛选流水线（必过）
**用例ID**: UI-TEST-002  
**优先级**: P0  
**页面**: `/pipeline`

**前置条件**: 存在多条流水线

**操作步骤**:
```javascript
// 1. 进入流水线列表页
await page.goto('/pipeline');
await page.waitForLoadState('networkidle');

// 2. 输入搜索关键词
await page.fill('.search-input', 'test');

// 3. 等待筛选结果
await page.waitForTimeout(500);
```

**预期结果**:
- 列表只显示包含"test"的流水线
- 无搜索结果时显示空状态

**验证方法**:
```javascript
// 检查列表项
const items = page.locator('.pipeline-item');
for (const item of await items.all()) {
  await expect(item).toContainText('test');
}
```

**边界测试**:
```javascript
// 空搜索
await page.fill('.search-input', '');
await expect(page.locator('.pipeline-item').first()).toBeVisible();

// 超长搜索
await page.fill('.search-input', 'a'.repeat(100));
// 验证无崩溃
```

---

### UI-TEST-003: Tab切换（必过）
**用例ID**: UI-TEST-003  
**优先级**: P0  
**页面**: `/pipeline`

**前置条件**: 用户创建了流水线，收藏了流水线

**操作步骤**:
```javascript
// 1. 进入流水线列表页
await page.goto('/pipeline');
await page.waitForLoadState('networkidle');

// 2. 点击"我创建的"Tab
await page.click('.el-tabs >> text=我创建的');
await page.waitForTimeout(300);

// 3. 点击"我收藏的"Tab
await page.click('.el-tabs >> text=我收藏的');
await page.waitForTimeout(300);

// 4. 点击"全部"Tab
await page.click('.el-tabs >> text=全部');
await page.waitForTimeout(300);
```

**预期结果**:
| Tab | 预期内容 |
|-----|---------|
| 全部 | 所有可访问的流水线 |
| 我创建的 | 只显示当前用户创建的流水线 |
| 我收藏的 | 只显示收藏的流水线 |

**验证方法**:
```javascript
// 验证"我创建的"Tab
await page.click('.el-tabs >> text=我创建');
await expect(page.locator('.pipeline-item').first()).toBeVisible();

// 验证"我收藏的"Tab
await page.click('.el-tabs >> text=我收藏');
await expect(page.locator('.favorite-icon').first()).toBeVisible();
```

---

### UI-TEST-004: 收藏流水线（必过）
**用例ID**: UI-TEST-004  
**优先级**: P0  
**页面**: `/pipeline`

**前置条件**: 存在未收藏的流水线

**操作步骤**:
```javascript
// 1. 进入流水线列表页
await page.goto('/pipeline');
await page.waitForLoadState('networkidle');

// 2. 悬停在第一个流水线卡片上
await page.locator('.pipeline-item').first().hover();

// 3. 点击收藏按钮
await page.locator('.pipeline-item').first().locator('.favorite-btn').click();

// 4. 验证收藏状态
```

**预期结果**:
- 收藏按钮变为实心（已收藏）
- 颜色变化（空心→实心）
- Toast提示"收藏成功"

**验证方法**:
```javascript
// 检查收藏按钮状态
const favoriteBtn = page.locator('.pipeline-item').first().locator('.favorite-btn');

// 点击前是空心
await expect(favoriteBtn.locator('.el-icon')).toHaveClass(/el-icon-star-off/);

// 点击
await favoriteBtn.click();

// 点击后是实心
await expect(favoriteBtn.locator('.el-icon')).toHaveClass(/el-icon-star-on/);

// 验证Toast提示
await expect(page.locator('.el-message')).toContainText('收藏成功');
```

---

## 三、创建流水线测试

### UI-TEST-010: 进入创建页面（必过）
**用例ID**: UI-TEST-010  
**优先级**: P0  
**页面**: `/pipeline/create`

**操作步骤**:
```javascript
// 1. 在列表页点击"创建流水线"按钮
await page.goto('/pipeline');
await page.click('button:has-text("创建流水线")');

// 2. 等待页面跳转
await page.waitForURL('**/pipeline/create');
await page.waitForLoadState('networkidle');
```

**预期结果**:
| 元素 | 验证点 |
|------|--------|
| 页面标题 | "创建流水线" |
| 基本信息表单 | 可见 |
| 节点编辑器 | 可见 |
| 保存按钮 | 可见 |
| 取消按钮 | 可见 |

**验证方法**:
```javascript
// 检查URL
await expect(page).toHaveURL(/\/pipeline\/create$/);

// 检查标题
await expect(page.locator('h1')).toContainText('创建流水线');

// 检查表单
await expect(page.locator('.basic-info-form')).toBeVisible();
await expect(page.locator('.node-editor')).toBeVisible();

// 检查按钮
await expect(page.locator('button:has-text("保存")')).toBeVisible();
await expect(page.locator('button:has-text("取消")')).toBeVisible();
```

---

### UI-TEST-011: 填写基本信息（必过）
**用例ID**: UI-TEST-011  
**优先级**: P0  
**页面**: `/pipeline/create`

**操作步骤**:
```javascript
// 1. 进入创建页面
await page.goto('/pipeline/create');
await page.waitForLoadState('networkidle');

// 2. 填写名称
await page.fill('input[placeholder="请输入流水线名称"]', 'E2E测试流水线');

// 3. 填写描述
await page.fill('textarea[placeholder="请输入描述"]', '这是一个E2E测试流水线');

// 4. 选择项目
await page.click('.project-select');
await page.click('.el-select-dropdown >> text=测试项目');

// 5. 选择环境
await page.click('.environment-select');
await page.click('.el-select-dropdown >> text=生产环境');
```

**预期结果**:
- 名称输入框显示" E2E测试流水线"
- 描述输入框显示文本
- 项目下拉框显示"测试项目"
- 环境选择显示"生产环境"

**验证方法**:
```javascript
// 验证名称
await expect(page.locator('input[placeholder="请输入流水线名称"]')).toHaveValue('E2E测试流水线');

// 验证描述
await expect(page.fill('textarea[placeholder="请输入描述"]')).toHaveValue('这是一个E2E测试流水线');

// 验证项目选择
await expect(page.locator('.project-select .el-input__inner')).toContainText('测试项目');

// 验证环境选择
await expect(page.locator('.environment-select .el-input__inner')).toContainText('生产环境');
```

---

### UI-TEST-012: 添加节点（必过）
**用例ID**: UI-TEST-012  
**优先级**: P0  
**页面**: `/pipeline/create`

**操作步骤**:
```javascript
// 1. 进入创建页面
await page.goto('/pipeline/create');
await page.waitForLoadState('networkidle');

// 2. 点击"添加节点"按钮
await page.click('button:has-text("添加节点")');

// 3. 在弹出的表单中填写节点信息
await page.fill('input[node-id-input]', 'build');
await page.selectOption('select[node-type-select]', 'shell');
await page.fill('textarea[script-input]', 'echo "Hello World"');

// 4. 确认添加
await page.click('button:has-text("确定")');
```

**预期结果**:
- 节点出现在画布上
- 节点显示正确的ID和类型
- 节点可以拖拽

**验证方法**:
```javascript
// 验证节点出现
await expect(page.locator('.node-item >> text=build')).toBeVisible();

// 验证节点类型图标
await expect(page.locator('.node-item .node-type')).toContainText('Shell');

// 验证节点可拖拽
const node = page.locator('.node-item >> text=build');
const initialPosition = await node.boundingBox();
await node.dragTo(page.locator('.canvas'), { targetPosition: { x: 100, y: 100 } });
```

---

### UI-TEST-013: 添加依赖边（必过）
**用例ID**: UI-TEST-013  
**优先级**: P0  
**页面**: `/pipeline/create`

**前置条件**: 已添加2个节点

**操作步骤**:
```javascript
// 1. 进入创建页面（已添加build和test节点）
await page.goto('/pipeline/create');
await page.waitForLoadState('networkidle');

// 2. 假设已添加build和test节点
// 3. 点击"添加依赖"按钮
await page.click('button:has-text("添加依赖")');

// 4. 选择源节点和目标节点
await page.click('select[from-select]');
await page.click('.el-select-dropdown >> text=build');
await page.click('select[to-select]');
await page.click('.el-select-dropdown >> text=test');

// 5. 确认添加
await page.click('button:has-text("确定")');
```

**预期结果**:
- 依赖边出现在画布上
- 箭头从build指向test
- DAG图正确显示

**验证方法**:
```javascript
// 验证边出现
await expect(page.locator('.edge-item')).toBeVisible();

// 验证边的连接
const edge = page.locator('.edge-item').first();
await expect(edge).toContainText('build → test');
```

---

### UI-TEST-014: 可视化连线（推荐）
**用例ID**: UI-TEST-014  
**优先级**: P1  
**页面**: `/pipeline/create`

**操作步骤**:
```javascript
// 1. 进入创建页面（已添加build和test节点）
await page.goto('/pipeline/create');
await page.waitForLoadState('networkidle');

// 2. 拖拽连接点进行连线
const buildNode = page.locator('.node-item >> text=build');
const testNode = page.locator('.node-item >> text=test');

// 从build的输出点拖拽到test的输入点
await buildNode.locator('.output-point').dragTo(testNode.locator('.input-point'));
```

**预期结果**:
- 拖拽过程中显示连接线
- 释放后创建依赖边
- 边的颜色高亮

**验证方法**:
```javascript
// 验证边创建
await expect(page.locator('.edge-item')).toHaveCount(1);

// 验证连接正确
const edge = page.locator('.edge-item').first();
await expect(edge).toContainText('build → test');
```

---

### UI-TEST-015: 验证配置有效性（必过）
**用例ID**: UI-TEST-015  
**优先级**: P0  
**页面**: `/pipeline/create`

**操作步骤**:
```javascript
// 场景1: 不填写名称
await page.fill('input[placeholder="请输入流水线名称"]', '');

// 场景2: 不添加节点
// 假设已删除所有节点

// 场景3: 添加循环依赖
// 假设添加了 A→B, B→C, C→A

// 4. 点击保存按钮
await page.click('button:has-text("保存")');
```

**预期结果**:
| 场景 | 预期结果 |
|------|---------|
| 不填写名称 | 显示错误提示"请输入名称" |
| 不添加节点 | 显示错误提示"至少添加一个节点" |
| 循环依赖 | 显示错误提示"存在循环依赖" |

**验证方法**:
```javascript
// 验证名称为空
await page.click('button:has-text("保存")');
await expect(page.locator('.el-form-item__error')).toContainText('请输入流水线名称');
```

---

### UI-TEST-016: 保存流水线（必过）
**用例ID**: UI-TEST-016  
**优先级**: P0  
**页面**: `/pipeline/create`

**操作步骤**:
```javascript
// 1. 填写基本信息
await page.fill('input[placeholder="请输入流水线名称"]', 'E2E测试流水线');
await page.fill('textarea[placeholder="请输入描述"]', '测试描述');

// 2. 添加节点
await page.click('button:has-text("添加节点")');
await page.fill('input[node-id-input]', 'build');
await page.selectOption('select[node-type-select]', 'shell');
await page.fill('textarea[script-input]', 'echo "build"');
await page.click('button:has-text("确定")');

// 3. 点击保存
await page.click('button:has-text("保存")');

// 4. 等待保存完成
await page.waitForTimeout(1000);
```

**预期结果**:
- 显示Toast提示"保存成功"
- 跳转到流水线详情页
- 页面URL包含流水线ID

**验证方法**:
```javascript
// 验证Toast提示
await expect(page.locator('.el-message')).toContainText('保存成功');

// 验证跳转
await expect(page).toHaveURL(/\/pipeline\/\d+$/);

// 验证页面显示
await expect(page.locator('h1')).toContainText('E2E测试流水线');
```

---

## 四、流水线详情页测试

### UI-TEST-020: 进入详情页（必过）
**用例ID**: UI-TEST-020  
**优先级**: P0  
**页面**: `/pipeline/:id`

**前置条件**: 存在ID为1的流水线

**操作步骤**:
```javascript
// 1. 点击流水线卡片
await page.goto('/pipeline');
await page.click('.pipeline-item >> text=E2E测试流水线');

// 2. 等待页面加载
await page.waitForLoadState('networkidle');
```

**预期结果**:
| 元素 | 验证点 |
|------|--------|
| 页面标题 | 显示流水线名称 |
| 基本信息 | 显示创建者、创建时间等 |
| 配置快照 | 显示PipelineConfig |
| 执行历史 | 显示历史记录 |
| 操作按钮 | 执行、编辑、删除 |

**验证方法**:
```javascript
// 检查URL
await expect(page).toHaveURL(/\/pipeline\/\d+$/);

// 检查标题
await expect(page.locator('h1')).toContainText('E2E测试流水线');

// 检查Tab
await expect(page.locator('.el-tabs >> text=配置')).toBeVisible();
await expect(page.locator('.el-tabs >> text=执行历史')).toBeVisible();
await expect(page.locator('.el-tabs >> text=执行详情')).toBeVisible();

// 检查操作按钮
await expect(page.locator('button:has-text("执行")')).toBeVisible();
await expect(page.locator('button:has-text("编辑")')).toBeVisible();
await expect(page.locator('button:has-text("删除")')).toBeVisible();
```

---

### UI-TEST-021: 查看执行历史（必过）
**用例ID**: UI-TEST-021  
**优先级**: P0  
**页面**: `/pipeline/:id`

**前置条件**: 流水线有多次执行记录

**操作步骤**:
```javascript
// 1. 进入详情页
await page.goto('/pipeline/1');
await page.waitForLoadState('networkidle');

// 2. 点击"执行历史"Tab
await page.click('.el-tabs >> text=执行历史');

// 3. 等待历史列表加载
await page.waitForTimeout(500);
```

**预期结果**:
| 列名 | 验证点 |
|------|--------|
| Build号 | 显示正确的BuildNumber |
| 状态 | 显示success/failed/running |
| 触发方式 | 显示manual/webhook/schedule |
| 执行时间 | 显示开始和结束时间 |
| 耗时 | 显示执行时长 |
| 操作 | 查看详情、重试按钮 |

**验证方法**:
```javascript
// 检查历史列表
const historyItems = page.locator('.history-item');
await expect(historyItems.first()).toBeVisible();

// 检查列
await expect(historyItems.first().locator('.build-number')).toContainText('#1');
await expect(historyItems.first().locator('.status')).toContainText(/success|failed|running/);
await expect(historyItems.first().locator('.trigger-type')).toContainText(/manual|webhook|schedule/);
```

---

### UI-TEST-022: 查看执行详情（必过）
**用例ID**: UI-TEST-022  
**优先级**: P0  
**页面**: `/pipeline/:id/execution/:runId`

**前置条件**: 存在执行记录

**操作步骤**:
```javascript
// 1. 进入详情页
await page.goto('/pipeline/1');
await page.waitForLoadState('networkidle');

// 2. 点击"执行详情"Tab
await page.click('.el-tabs >> text=执行详情');

// 3. 点击某次执行的"查看详情"按钮
await page.click('.history-item:first-child >> text=查看详情');

// 4. 等待详情页加载
await page.waitForLoadState('networkidle');
```

**预期结果**:
| 元素 | 验证点 |
|------|--------|
| DAG图 | 显示任务依赖关系 |
| 任务列表 | 显示所有任务及其状态 |
| 执行时间 | 显示开始/结束时间 |
| 触发信息 | 显示触发者和方式 |

**验证方法**:
```javascript
// 检查DAG图
await expect(page.locator('.dag-graph')).toBeVisible();

// 检查任务列表
const tasks = page.locator('.task-item');
await expect(tasks.first()).toBeVisible();

// 检查任务状态
await expect(tasks.first().locator('.status')).toContainText(/success|failed|running|pending/);
```

---

### UI-TEST-023: 查看实时日志（必过）
**用例ID**: UI-TEST-023  
**优先级**: P0  
**页面**: `/pipeline/:id/execution/:runId`

**前置条件**: 流水线正在执行中

**操作步骤**:
```javascript
// 1. 进入执行详情页
await page.goto('/pipeline/1/execution/1');
await page.waitForLoadState('networkidle');

// 2. 点击正在执行的任务
await page.click('.task-item.running');

// 3. 展开日志面板
await page.click('.log-panel-tab >> text=日志');
```

**预期结果**:
- 日志实时更新
- 显示时间戳
- 显示日志内容
- 区分stdout和stderr

**验证方法**:
```javascript
// 检查日志面板
await expect(page.locator('.log-panel')).toBeVisible();

// 检查日志内容
const logLines = page.locator('.log-line');
await expect(logLines.first()).toBeVisible();

// 验证日志格式
const firstLog = logLines.first();
await expect(firstLog).toContainText(/\[\d{2}:\d{2}:\d{2}\]/);  // 时间戳
```

---

### UI-TEST-024: 日志搜索（推荐）
**用例ID**: UI-TEST-024  
**优先级**: P1  
**页面**: `/pipeline/:id/execution/:runId`

**前置条件**: 任务已执行完成，日志较多

**操作步骤**:
```javascript
// 1. 进入执行详情页
await page.goto('/pipeline/1/execution/1');
await page.waitForLoadState('networkidle');

// 2. 点击任务，展开日志
await page.click('.task-item >> text=build');
await page.click('.log-panel-tab >> text=日志');

// 3. 在搜索框输入关键词
await page.fill('.log-search-input', 'error');

// 4. 等待筛选
await page.waitForTimeout(300);
```

**预期结果**:
- 只显示包含"error"的日志行
- 高亮匹配的关键词

**验证方法**:
```javascript
// 检查筛选结果
const logLines = page.locator('.log-line');
for (const line of await logLines.all()) {
  await expect(line).toContainText('error');
}
```

---

### UI-TEST-025: 重试执行（必过）
**用例ID**: UI-TEST-025  
**优先级**: P0  
**页面**: `/pipeline/:id`

**前置条件**: 存在失败的执行记录

**操作步骤**:
```javascript
// 1. 进入详情页
await page.goto('/pipeline/1');
await page.waitForLoadState('networkidle');

// 2. 点击"执行历史"Tab
await page.click('.el-tabs >> text=执行历史');

// 3. 找到失败的执行记录
const failedRun = page.locator('.history-item').filter({ hasText: '失败' }).first();

// 4. 点击"重试"按钮
await failedRun.locator('button:has-text("重试")').click();

// 5. 确认重试
await page.click('.el-message-box button:has-text("确定")');
```

**预期结果**:
- 显示确认对话框
- 确认后创建新的执行
- 跳转到新执行详情页
- 显示Toast提示"已开始执行"

**验证方法**:
```javascript
// 检查确认对话框
await expect(page.locator('.el-message-box')).toBeVisible();
await expect(page.locator('.el-message-box__content')).toContainText('确认重试');

// 确认后
await page.click('.el-message-box button:has-text("确定")');

// 验证新执行创建
await expect(page.locator('.el-message')).toContainText('已开始执行');

// 验证跳转
await expect(page).toHaveURL(/\/pipeline\/\d+\/execution\/\d+$/);
```

---

## 五、流水线执行测试

### UI-TEST-030: 手动触发执行（必过）
**用例ID**: UI-TEST-030  
**优先级**: P0  
**页面**: `/pipeline/:id`

**前置条件**: 存在有效的流水线配置

**操作步骤**:
```javascript
// 1. 进入详情页
await page.goto('/pipeline/1');
await page.waitForLoadState('networkidle');

// 2. 点击"执行"按钮
await page.click('button:has-text("执行")');

// 3. 在确认对话框中点击"确定"
await page.click('.el-message-box button:has-text("确定")');
```

**预期结果**:
- 显示确认对话框
- 确认后显示Toast"已开始执行"
- 执行历史中出现新的执行记录
- 状态变为"执行中"

**验证方法**:
```javascript
// 检查确认对话框
await expect(page.locator('.el-message-box')).toContainText('确认执行');

// 确认执行
await page.click('.el-message-box button:has-text("确定")');

// 检查Toast
await expect(page.locator('.el-message')).toContainText('已开始执行');

// 检查状态更新
await expect(page.locator('.status-tag')).toContainText('执行中');
```

---

### UI-TEST-031: 查看执行进度（必过）
**用例ID**: UI-TEST-031  
**优先级**: P0  
**页面**: `/pipeline/:id/execution/:runId`

**前置条件**: 流水线正在执行中

**操作步骤**:
```javascript
// 1. 进入执行详情页
await page.goto('/pipeline/1/execution/2');
await page.waitForLoadState('networkidle');

// 2. 查看进度信息
const progress = page.locator('.execution-progress');
```

**预期结果**:
- 显示执行进度百分比
- 显示当前正在执行的任务
- 显示已完成的节点数/总节点数
- 进度条动态更新

**验证方法**:
```javascript
// 检查进度信息
await expect(progress.locator('.progress-text')).toContainText(/\d+\/\d+/);

// 检查进度条
const progressBar = progress.locator('.el-progress-bar');
await expect(progressBar).toBeVisible();

// 检查当前任务
await expect(progress.locator('.current-task')).toContainText(/执行中|pending/);
```

---

### UI-TEST-032: 取消执行（必过）
**用例ID**: UI-TEST-032  
**优先级**: P0  
**页面**: `/pipeline/:id/execution/:runId`

**前置条件**: 流水线正在执行中

**操作步骤**:
```javascript
// 1. 进入执行详情页
await page.goto('/pipeline/1/execution/2');
await page.waitForLoadState('networkidle');

// 2. 点击"停止"按钮
await page.click('button:has-text("停止")');

// 3. 确认停止
await page.click('.el-message-box button:has-text("确定")');
```

**预期结果**:
- 显示确认对话框
- 确认后停止所有正在执行的任务
- 执行状态变为"已取消"
- 停止原因记录

**验证方法**:
```javascript
// 检查确认对话框
await expect(page.locator('.el-message-box')).toContainText('确认停止');

// 确认停止
await page.click('.el-message-box button:has-text("确定")');

// 检查状态更新
await expect(page.locator('.status-tag')).toContainText('已取消');

// 检查停止原因
await expect(page.locator('.error-message')).toContainText('用户取消');
```

---

## 六、流水线编辑测试

### UI-TEST-040: 进入编辑页面（必过）
**用例ID**: UI-TEST-040  
**优先级**: P0  
**页面**: `/pipeline/:id/edit`

**前置条件**: 用户是流水线创建者或管理员

**操作步骤**:
```javascript
// 1. 进入流水线详情页
await page.goto('/pipeline/1');
await page.waitForLoadState('networkidle');

// 2. 点击"编辑"按钮
await page.click('button:has-text("编辑")');

// 3. 等待页面跳转
await page.waitForURL('**/pipeline/1/edit');
await page.waitForLoadState('networkidle');
```

**预期结果**:
| 元素 | 验证点 |
|------|--------|
| 页面标题 | "编辑流水线" |
| 加载现有配置 | 显示当前配置 |
| 节点编辑器 | 显示当前节点 |
| 保存按钮 | 可见 |

**验证方法**:
```javascript
// 检查URL
await expect(page).toHaveURL(/\/pipeline\/\d+\/edit$/);

// 检查标题
await expect(page.locator('h1')).toContainText('编辑流水线');

// 检查配置加载
await expect(page.locator('input[placeholder="请输入流水线名称"]')).toHaveValue('原有名称');

// 检查节点加载
const nodes = page.locator('.node-item');
await expect(nodes.first()).toBeVisible();
```

---

### UI-TEST-041: 修改节点配置（必过）
**用例ID**: UI-TEST-041  
**优先级**: P0  
**页面**: `/pipeline/:id/edit`

**操作步骤**:
```javascript
// 1. 进入编辑页面
await page.goto('/pipeline/1/edit');
await page.waitForLoadState('networkidle');

// 2. 点击要修改的节点
await page.click('.node-item >> text=build');

// 3. 修改节点配置
await page.fill('textarea[script-input]', 'echo "修改后的脚本"');

// 4. 保存修改
await page.click('button:has-text("保存")');
```

**预期结果**:
- 节点配置面板打开
- 脚本修改成功
- 保存成功
- 跳转到详情页

**验证方法**:
```javascript
// 检查配置面板
await expect(page.locator('.node-config-panel')).toBeVisible();

// 修改脚本
await page.fill('textarea[script-input]', 'echo "修改后的脚本"');

// 保存
await page.click('button:has-text("保存")');

// 验证保存成功
await expect(page.locator('.el-message')).toContainText('保存成功');
await expect(page).toHaveURL(/\/pipeline\/\d+$/);
```

---

### UI-TEST-042: 修改依赖关系（必过）
**用例ID**: UI-TEST-042  
**优先级**: P0  
**页面**: `/pipeline/:id/edit`

**前置条件**: 当前配置A→B→C

**操作步骤**:
```javascript
// 1. 进入编辑页面
await page.goto('/pipeline/1/edit');
await page.waitForLoadState('networkidle');

// 2. 删除现有依赖
await page.click('.edge-item');
await page.click('button:has-text("删除")');

// 3. 添加新的依赖A→C
await page.click('button:has-text("添加依赖")');
await page.selectOption('select[from-select]', 'build');
await page.selectOption('select[to-select]', 'deploy');
await page.click('button:has-text("确定")');

// 4. 保存
await page.click('button:has-text("保存")');
```

**预期结果**:
- 原有依赖A→B→C变为A→C
- 跳过节点B
- 保存成功

**验证方法**:
```javascript
// 验证依赖变更
const edges = page.locator('.edge-item');
await expect(edges).toHaveCount(1);

// 验证边内容
await expect(edges.first()).toContainText('build → deploy');

// 保存成功
await page.click('button:has-text("保存")');
await expect(page.locator('.el-message')).toContainText('保存成功');
```

---

## 七、流水线删除测试

### UI-TEST-050: 删除确认（必过）
**用例ID**: UI-TEST-050  
**优先级**: P0  
**页面**: `/pipeline/:id`

**前置条件**: 用户有删除权限

**操作步骤**:
```javascript
// 1. 进入详情页
await page.goto('/pipeline/1');
await page.waitForLoadState('networkidle');

// 2. 点击"删除"按钮
await page.click('button:has-text("删除")');

// 3. 观察确认对话框
```

**预期结果**:
| 场景 | 预期结果 |
|------|---------|
| 无执行历史 | 显示普通确认对话框 |
| 有执行历史 | 显示警告对话框，包含执行记录数量 |

**验证方法**:
```javascript
// 点击删除
await page.click('button:has-text("删除")');

// 检查确认对话框
await expect(page.locator('.el-message-box')).toBeVisible();

// 如果有执行历史，显示警告
if (await page.locator('.el-message-box >> text=执行记录').isVisible()) {
  await expect(page.locator('.el-message-box')).toContainText('该流水线有');
  await expect(page.locator('.el-message-box')).toContainText('条执行记录');
}
```

---

### UI-TEST-051: 确认删除（必过）
**用例ID**: UI-TEST-051  
**优先级**: P0  
**页面**: `/pipeline/:id`

**操作步骤**:
```javascript
// 1. 进入详情页
await page.goto('/pipeline/1');
await page.waitForLoadState('networkidle');

// 2. 点击"删除"按钮
await page.click('button:has-text("删除")');

// 3. 在确认对话框中点击"确定"
await page.click('.el-message-box button:has-text("确定")');

// 4. 等待跳转
await page.waitForTimeout(1000);
```

**预期结果**:
- 确认删除后跳转到流水线列表页
- 流水线从列表中消失
- 显示Toast提示"删除成功"

**验证方法**:
```javascript
// 确认删除
await page.click('.el-message-box button:has-text("确定")');

// 验证跳转
await expect(page).toHaveURL('/pipeline');

// 验证Toast
await expect(page.locator('.el-message')).toContainText('删除成功');

// 验证从列表中移除
await expect(page.locator('.pipeline-item >> text=E2E测试流水线')).toBeHidden();
```

---

### UI-TEST-052: 取消删除（必过）
**用例ID**: UI-TEST-052  
**优先级**: P0  
**页面**: `/pipeline/:id`

**操作步骤**:
```javascript
// 1. 进入详情页
await page.goto('/pipeline/1');
await page.waitForLoadState('networkidle');

// 2. 点击"删除"按钮
await page.click('button:has-text("删除")');

// 3. 在确认对话框中点击"取消"
await page.click('.el-message-box button:has-text("取消")');
```

**预期结果**:
- 对话框关闭
- 流水线未删除
- 仍可正常访问详情页

**验证方法**:
```javascript
// 取消删除
await page.click('.el-message-box button:has-text("取消")');

// 对话框关闭
await expect(page.locator('.el-message-box')).toBeHidden();

// 仍在详情页
await expect(page).toHaveURL(/\/pipeline\/\d+$/);
await expect(page.locator('h1')).toContainText('E2E测试流水线');
```

---

## 八、UI测试用例汇总

### 8.1 测试用例统计

| 模块 | P0用例 | P1用例 | 小计 |
|------|--------|--------|------|
| 流水线列表页 | 4 | 0 | 4 |
| 创建流水线 | 7 | 1 | 8 |
| 流水线详情页 | 6 | 1 | 7 |
| 流水线执行 | 3 | 0 | 3 |
| 流水线编辑 | 3 | 0 | 3 |
| 流水线删除 | 3 | 0 | 3 |
| **总计** | **26** | **2** | **28** |

### 8.2 测试覆盖情况

| 页面 | 测试覆盖 |
|------|---------|
| `/pipeline` | ✅ 列表、筛选、Tab、收藏 |
| `/pipeline/create` | ✅ 基本信息、节点、依赖、验证、保存 |
| `/pipeline/:id` | ✅ 详情、历史、详情、日志、重试 |
| `/pipeline/:id/edit` | ✅ 编辑、修改、依赖变更 |
| `/pipeline/:id/execution/:runId` | ✅ 进度、日志、搜索、停止 |

---

## 九、Playwright测试脚本模板

### 9.1 登录脚本

```javascript
// tests/login.spec.js
import { test, expect } from '@playwright/test';

test.describe('登录', () => {
  test('使用管理员账户登录', async ({ page }) => {
    // 1. 进入登录页
    await page.goto('/login');
    await page.waitForLoadState('networkidle');

    // 2. 输入用户名和密码
    await page.fill('input[placeholder="用户名"]', 'admin');
    await page.fill('input[placeholder="密码"]', '1qaz2WSX');

    // 3. 点击登录按钮
    await page.click('button:has-text("登录")');

    // 4. 验证登录成功
    await expect(page).toHaveURL('/dashboard');
    await expect(page.locator('.user-info')).toContainText('admin');
  });
});
```

### 9.2 创建流水线脚本

```javascript
// tests/pipeline/create.spec.js
import { test, expect } from '@playwright/test';

test.describe('创建流水线', () => {
  test.beforeEach(async ({ page }) => {
    // 登录
    await page.goto('/login');
    await page.fill('input[placeholder="用户名"]', 'admin');
    await page.fill('input[placeholder="密码"]', '1qaz2WSX');
    await page.click('button:has-text("登录")');
    await page.waitForURL('/dashboard');
  });

  test('创建有效的流水线', async ({ page }) => {
    // 1. 进入创建页面
    await page.goto('/pipeline/create');
    await page.waitForLoadState('networkidle');

    // 2. 填写基本信息
    await page.fill('input[placeholder="请输入流水线名称"]', '自动化测试流水线');
    await page.fill('textarea[placeholder="请输入描述"]', '用于自动化测试');

    // 3. 添加节点
    await page.click('button:has-text("添加节点")');
    await page.fill('input[node-id-input]', 'build');
    await page.selectOption('select[node-type-select]', 'shell');
    await page.fill('textarea[script-input]', 'npm install && npm run build');
    await page.click('button:has-text("确定")');

    // 4. 添加另一个节点
    await page.click('button:has-text("添加节点")');
    await page.fill('input[node-id-input]', 'test');
    await page.selectOption('select[node-type-select]', 'shell');
    await page.fill('textarea[script-input]', 'npm run test');
    await page.click('button:has-text("确定")');

    // 5. 添加依赖
    await page.click('button:has-text("添加依赖")');
    await page.selectOption('select[from-select]', 'build');
    await page.selectOption('select[to-select]', 'test');
    await page.click('button:has-text("确定")');

    // 6. 保存
    await page.click('button:has-text("保存")');

    // 7. 验证保存成功
    await expect(page.locator('.el-message')).toContainText('保存成功');
    await expect(page).toHaveURL(/\/pipeline\/\d+$/);
  });
});
```

---

## 十、运行测试

### 10.1 运行所有UI测试

```bash
cd /Users/wangshengpeng/work/code/easydo3
npm install
npx playwright install
npx playwright test
```

### 10.2 运行特定测试

```bash
# 运行创建流水线测试
npx playwright test tests/pipeline/create.spec.js

# 运行详情页测试
npx playwright test tests/pipeline/detail.spec.js

# 运行带追踪的测试
npx playwright test --reporter=line --trace=on
```

### 10.3 打开测试报告

```bash
npx playwright show-report
```

---

**文档版本**: 1.0  
**创建日期**: 2026-02-02  
**测试工具**: Playwright  
**涵盖页面**: 流水线列表、创建、详情、执行、编辑、删除
