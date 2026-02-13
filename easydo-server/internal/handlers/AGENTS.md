# Handlers Layer

业务逻辑层。**无Service层**，Handler直接访问DB。

## FILES

| File | Purpose |
|------|---------|
| `user.go` | 用户认证、注册、信息 |
| `project.go` | 项目CRUD、收藏 |
| `pipeline.go` | 流水线管理 |

## PATTERN

```go
type ProjectHandler struct {
    DB *gorm.DB
}

func NewProjectHandler() *ProjectHandler {
    return &ProjectHandler{DB: models.DB}
}
```

## GOTCHAS

- 全局DB: `models.DB`
- 无事务封装
- 400/500错误码混合使用
- SQL注入风险: 使用`strings.Join`拼SQL (project.go)
