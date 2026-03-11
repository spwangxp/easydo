# EasyDo Project Knowledge Base


## BASE CONSENSUS

以下为基础共识，所有开发者必须遵守：

1. **代码组织**：
   - 前端代码 → `easydo-frontend/`
   - 后端代码 → `easydo-server/`
   - 执行器agent代码 -> `easydo-agent/`

2. **开发规范**：
   - 编译调试必须使用 Docker 在容器内完成编译运行后进行, 
   - ❌ **禁止**直接在机器上通过命令操作（除非必要）
   - 禁止读取操作其他目录的内容，只能读取当前工作目录的内容
   - 在需要调试的时候，需要先停止旧容器，删除旧镜像，再进行编译部署，需要确保部署的是最新代码
   - 调试先通过操作浏览器界面进行测试，界面交互逻辑通过后，还需要对接口进行测试，当前后端都测试通过后才算测试通过
   - 无需保留截图证据,非必要就不要保留截图； 如果验证阶段必须截图，保存路径为 screenshots 目录中 
   - 没有成本限制，不要考虑折中或者临时方案，必须按照最终目标实现。

3. **技术栈要求**：
   - 前端：Vue 3 + Composition API
   - 后端：Golang + Gin框架
   - **每个模块必须包含Dockerfile**

4. **数据库**：
   - MySQL（主数据库）
   - Redis（缓存/会话）


### 技术栈
- **前端:** Vue 3 + Composition API + Element Plus + Vite
- **后端:** Go 1.21 + Gin + GORM + MySQL + Redis
- **每个模块必须包含Dockerfile**


## ANTI-PATTERNS

- ❌ 禁止直接操作数据库，必须通过Docker
- ❌ 禁止在生产环境调用 `autoMigrate()` (当前在InitDB中)
- ❌ 禁止在 models.go 添加业务逻辑
- ❌ 禁止跳过 lint 检查

## TEST ACCOUNTS

- **demo** / 1qaz2WSX (普通用户)
- **admin** / 1qaz2WSX (管理员)
- **test** / 1qaz2WSX (普通用户)
