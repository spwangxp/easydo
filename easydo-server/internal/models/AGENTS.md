# Models Layer

GORM模型定义 + 全局DB初始化。

## FILES

| File | Purpose |
|------|---------|
| `models.go` | DB初始化、迁移后 schema 校验 |
| `user.go` | 用户模型 |
| `project.go` | 项目模型 |
| `pipeline.go` | 流水线模型 |
| `pipelinerun.go` | 流水线运行记录 |
| `deployrecord.go` | 发布记录 |

## BASE MODEL

```go
type BaseModel struct {
    ID        uint64    `gorm:"primarykey" json:"id"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}
```

## SCHEMA BOOTSTRAP

数据库结构和初始化数据由 server 内置的 Flyway 风格 SQL 迁移负责，`models.go` 不再承担运行时建表或测试数据初始化职责。

## GOTCHAS

- `InitDB()` 只负责连接数据库、校验迁移后的 schema，并初始化运行时所需的 `master_keys`
- 结构变更和初始化数据必须通过 server 内置的版本化 SQL 迁移维护，不能回退到运行时自动建表/写种子数据
- 全局变量: `var DB *gorm.DB`
- 密码加密: `user.SetPassword()`
