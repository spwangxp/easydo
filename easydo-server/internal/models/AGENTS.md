# Models Layer

GORM模型定义 + 全局DB初始化。

## FILES

| File | Purpose |
|------|---------|
| `models.go` | DB初始化、autoMigrate、测试数据 |
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

## SEEDING (首次启动时)

测试数据在 `autoMigrate()` 中调用，通过 `count > 0` 检查确保只初始化一次：

```go
func initTestUsers() {
    var count int64
    DB.Model(&User{}).Count(&count)
    if count > 0 {
        return // 已存在用户，跳过初始化
    }
    // 插入 demo/admin/test 用户...
}
```

## GOTCHAS

- `autoMigrate()` 在 `InitDB()` 中调用，开发环境每次启动都会执行
- 测试数据 seeding 在 `autoMigrate()` 中，首次启动后跳过
- 全局变量: `var DB *gorm.DB`
- 密码加密: `user.SetPassword()`
