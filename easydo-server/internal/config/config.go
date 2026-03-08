package config

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/viper"
)

var Config *viper.Viper

func Init() {
	Config = viper.New()

	// 1. 首先设置默认值
	Config.SetDefault("server.port", 8080)
	Config.SetDefault("server.mode", "debug")
	Config.SetDefault("database.driver", "mysql")
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

	Config.BindEnv("database.host", "DB_HOST")
	Config.BindEnv("database.port", "DB_PORT")
	Config.BindEnv("database.username", "DB_USERNAME")
	Config.BindEnv("database.password", "DB_PASSWORD")
	Config.BindEnv("database.name", "DB_NAME")

	Config.BindEnv("redis.host", "REDIS_HOST")
	Config.BindEnv("redis.port", "REDIS_PORT")
	Config.BindEnv("redis.password", "REDIS_PASSWORD")
	Config.BindEnv("redis.db", "REDIS_DB")

	Config.BindEnv("jwt.secret", "JWT_SECRET")
	Config.BindEnv("jwt.expire", "JWT_EXPIRE")
	Config.BindEnv("auth.token_ttl", "AUTH_TOKEN_TTL")
	Config.BindEnv("auth.refresh_interval", "AUTH_REFRESH_INTERVAL")
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

// GetEnvWithFallback 获取环境变量，如果不存在则返回默认值
func GetEnvWithFallback(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
