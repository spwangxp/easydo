package models

import (
	"easydo-server/internal/config"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

type BaseModel struct {
	ID        uint64    `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func InitDB() {
	var err error
	DB, err = gorm.Open(mysql.Open(config.GetDSN()), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})

	if err != nil {
		panic("Failed to connect to database: " + err.Error())
	}

	// 配置连接池
	sqlDB, err := DB.DB()
	if err != nil {
		panic("Failed to get underlying sql.DB: " + err.Error())
	}

	sqlDB.SetMaxOpenConns(config.Config.GetInt("database.max_open_conns"))
	sqlDB.SetMaxIdleConns(config.Config.GetInt("database.max_idle_conns"))
	sqlDB.SetConnMaxLifetime(config.Config.GetDuration("database.max_life_time"))

	// 自动迁移
	autoMigrate()

	// 加载或创建主密钥（持久化在数据库）
	if _, err := LoadOrCreateMasterKey(DB); err != nil {
		panic("Failed to initialize master key: " + err.Error())
	}
}

func autoMigrate() {
	DB.AutoMigrate(
		&User{},
		&Project{},
		&Pipeline{},
		&PipelineRun{},
		&DeployRecord{},
		&Agent{},
		&AgentTask{},
		&TaskExecution{},
		&AgentHeartbeat{},
		&AgentLog{},
		&AgentTaskEvent{},
		&AgentLogChunk{},
		&Secret{},
		&SecretUsage{},
		&SecretAuditLog{},
		&SecretRotation{},
		&WebhookConfig{},
		&WebhookEvent{},
		&SecretPermission{},
		&Credential{},
		&CredentialUsage{},
		&CredentialAuditLog{},
		&MasterKey{},
	)

	// 初始化测试用户
	initTestUsers()
	// 初始化测试项目
	initTestProjects()
}

func initTestUsers() {
	var count int64
	DB.Model(&User{}).Count(&count)
	if count > 0 {
		return // 已存在用户，跳过初始化
	}

	testUsers := []User{
		{
			Username: "demo",
			Nickname: "Demo用户",
			Email:    "demo@example.com",
			Role:     "admin",
			Status:   "active",
		},
		{
			Username: "admin",
			Nickname: "管理员",
			Email:    "admin@example.com",
			Role:     "admin",
			Status:   "active",
		},
		{
			Username: "test",
			Nickname: "测试用户",
			Email:    "test@example.com",
			Role:     "user",
			Status:   "active",
		},
	}

	for i := range testUsers {
		if err := testUsers[i].SetPassword("1qaz2WSX"); err != nil {
			continue
		}
		DB.Create(&testUsers[i])
	}
}

func initTestProjects() {
	var count int64
	DB.Model(&Project{}).Count(&count)
	if count > 0 {
		return // 已存在项目，跳过初始化
	}

	// 获取第一个用户ID作为所有者
	var user User
	if err := DB.First(&user).Error; err != nil {
		return
	}

	testProjects := []Project{
		{
			Name:        "默认项目",
			Description: "系统默认创建的项目",
			Color:       "#409EFF",
			OwnerID:     user.ID,
		},
		{
			Name:        "前端项目",
			Description: "包含所有前端代码的仓库",
			Color:       "#67C23A",
			OwnerID:     user.ID,
		},
		{
			Name:        "后端项目",
			Description: "包含所有后端API和服务的仓库",
			Color:       "#E6A23C",
			OwnerID:     user.ID,
		},
	}

	for i := range testProjects {
		DB.Create(&testProjects[i])
	}
}
