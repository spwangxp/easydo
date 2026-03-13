package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

var Config *viper.Viper

func Init() {
	Config = viper.New()

	// 1. 首先设置默认值
	Config.SetDefault("server.port", 8080)
	Config.SetDefault("server.mode", "release")
	Config.SetDefault("server.id", "")
	Config.SetDefault("server.internal_url", "")
	Config.SetDefault("server.internal_token", "")
	Config.SetDefault("database.driver", "mysql")
	Config.SetDefault("database.auto_migrate", false)
	Config.SetDefault("database.seed_test_data", false)
	Config.SetDefault("database.port", 3306)
	Config.SetDefault("database.max_open_conns", 100)
	Config.SetDefault("database.max_idle_conns", 10)
	Config.SetDefault("database.max_life_time", time.Hour)
	Config.SetDefault("redis.port", 6379)
	Config.SetDefault("redis.password", "")
	Config.SetDefault("redis.db", 0)
	Config.SetDefault("redis.pool_size", 10)
	Config.SetDefault("jwt.expire", 24*time.Hour)
	Config.SetDefault("auth.token_ttl", 4*time.Hour)
	Config.SetDefault("auth.refresh_interval", 10*time.Minute)
	Config.SetDefault("task.lease_ttl", 2*time.Minute)
	Config.SetDefault("task.dispatch_timeout", 30*time.Second)
	Config.SetDefault("logging.live_buffer_max_chunks", 1024)
	Config.SetDefault("logging.segment_max_lines", 200)
	Config.SetDefault("logging.segment_flush_interval", 5*time.Second)
	Config.SetDefault("object_storage.provider", "s3")
	Config.SetDefault("object_storage.endpoint", "")
	Config.SetDefault("object_storage.bucket", "easydo-logs")
	Config.SetDefault("object_storage.use_ssl", false)
	Config.SetDefault("object_storage.access_key", "")
	Config.SetDefault("object_storage.secret_key", "")
	Config.SetDefault("object_storage.region", "us-east-1")

	// 2. 从配置文件读取
	Config.SetConfigName("config")
	Config.SetConfigType("yaml")
	Config.AddConfigPath(".")
	Config.AddConfigPath("/app")

	if err := Config.ReadInConfig(); err != nil {
		// 配置文件不存在或解析失败，使用默认值
	}

	// 3. 环境变量覆盖 (最高优先级)
	// 格式: Config.BindEnv("key", "ENV_VAR_NAME")
	Config.BindEnv("server.port", "SERVER_PORT")
	Config.BindEnv("server.mode", "SERVER_MODE")
	Config.BindEnv("server.id", "SERVER_ID")
	Config.BindEnv("server.internal_url", "SERVER_INTERNAL_URL")
	Config.BindEnv("server.internal_token", "SERVER_INTERNAL_TOKEN")

	Config.BindEnv("database.host", "DB_HOST")
	Config.BindEnv("database.port", "DB_PORT")
	Config.BindEnv("database.username", "DB_USERNAME")
	Config.BindEnv("database.password", "DB_PASSWORD")
	Config.BindEnv("database.name", "DB_NAME")
	Config.BindEnv("database.auto_migrate", "DB_AUTO_MIGRATE")
	Config.BindEnv("database.seed_test_data", "DB_SEED_TEST_DATA")

	Config.BindEnv("redis.host", "REDIS_HOST")
	Config.BindEnv("redis.port", "REDIS_PORT")
	Config.BindEnv("redis.password", "REDIS_PASSWORD")
	Config.BindEnv("redis.db", "REDIS_DB")

	Config.BindEnv("jwt.secret", "JWT_SECRET")
	Config.BindEnv("jwt.expire", "JWT_EXPIRE")
	Config.BindEnv("auth.token_ttl", "AUTH_TOKEN_TTL")
	Config.BindEnv("auth.refresh_interval", "AUTH_REFRESH_INTERVAL")
	Config.BindEnv("task.lease_ttl", "TASK_LEASE_TTL")
	Config.BindEnv("task.dispatch_timeout", "TASK_DISPATCH_TIMEOUT")
	Config.BindEnv("logging.live_buffer_max_chunks", "LOG_LIVE_BUFFER_MAX_CHUNKS")
	Config.BindEnv("logging.segment_max_lines", "LOG_SEGMENT_MAX_LINES")
	Config.BindEnv("logging.segment_flush_interval", "LOG_SEGMENT_FLUSH_INTERVAL")
	Config.BindEnv("object_storage.provider", "OBJECT_STORAGE_PROVIDER")
	Config.BindEnv("object_storage.endpoint", "OBJECT_STORAGE_ENDPOINT")
	Config.BindEnv("object_storage.bucket", "OBJECT_STORAGE_BUCKET")
	Config.BindEnv("object_storage.use_ssl", "OBJECT_STORAGE_USE_SSL")
	Config.BindEnv("object_storage.access_key", "OBJECT_STORAGE_ACCESS_KEY")
	Config.BindEnv("object_storage.secret_key", "OBJECT_STORAGE_SECRET_KEY")
	Config.BindEnv("object_storage.region", "OBJECT_STORAGE_REGION")
}

func GetDSN() string {
	host := Config.GetString("database.host")
	port := Config.GetInt("database.port")
	username := Config.GetString("database.username")
	password := Config.GetString("database.password")
	name := Config.GetString("database.name")

	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		username, password, host, port, name)
}

func ValidateMultiReplicaRequirements() error {
	missing := make([]string, 0, 3)
	if strings.TrimSpace(Config.GetString("server.id")) == "" {
		missing = append(missing, "server.id")
	}
	if strings.TrimSpace(Config.GetString("server.internal_url")) == "" {
		missing = append(missing, "server.internal_url")
	}
	if strings.TrimSpace(Config.GetString("server.internal_token")) == "" {
		missing = append(missing, "server.internal_token")
	}
	if len(missing) > 0 {
		return fmt.Errorf("multi-replica configuration missing required settings: %s", strings.Join(missing, ", "))
	}
	return nil
}

func ShouldAutoMigrate() bool {
	return Config.GetBool("database.auto_migrate")
}

func ShouldSeedTestData() bool {
	return Config.GetBool("database.seed_test_data")
}

func ServerMode() string {
	switch strings.ToLower(strings.TrimSpace(Config.GetString("server.mode"))) {
	case "debug", "release", "test":
		return strings.ToLower(strings.TrimSpace(Config.GetString("server.mode")))
	default:
		return "release"
	}
}

// GetEnvWithFallback 获取环境变量，如果不存在则返回默认值
func GetEnvWithFallback(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
