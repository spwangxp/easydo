package utils

import (
	"context"
	"encoding/json"
	"time"

	"easydo-server/internal/config"
	"github.com/redis/go-redis/v9"
)

var RedisClient *redis.Client

// LogBufferConfig holds configuration for log buffering
type LogBufferConfig struct {
	FlushInterval time.Duration
	MaxBatchSize  int
	KeyPrefix     string
}

func InitRedis() {
	RedisClient = redis.NewClient(&redis.Options{
		Addr:     config.Config.GetString("redis.host") + ":" + config.Config.GetString("redis.port"),
		Password: config.Config.GetString("redis.password"),
		DB:       config.Config.GetInt("redis.db"),
		PoolSize: config.Config.GetInt("redis.pool_size"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := RedisClient.Ping(ctx).Result()
	if err != nil {
		panic("Failed to connect to Redis: " + err.Error())
	}
}

func SetCache(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return RedisClient.Set(ctx, key, value, expiration).Err()
}

func GetCache(ctx context.Context, key string) (string, error) {
	return RedisClient.Get(ctx, key).Result()
}

func DeleteCache(ctx context.Context, key string) error {
	return RedisClient.Del(ctx, key).Err()
}

func Incr(ctx context.Context, key string) int64 {
	return RedisClient.Incr(ctx, key).Val()
}

func Expire(ctx context.Context, key string, expiration time.Duration) bool {
	return RedisClient.Expire(ctx, key, expiration).Val()
}

// LogBufferPrefix is the prefix for log buffer keys
const LogBufferPrefix = "logs:"

// PushLogToBuffer pushes a log entry to Redis buffer
func PushLogToBuffer(runID, taskID uint64, logData map[string]interface{}) error {
	ctx := context.Background()
	key := LogBufferPrefix + formatUint64(runID) + ":" + formatUint64(taskID)

	logJSON, err := json.Marshal(logData)
	if err != nil {
		return err
	}

	// Use LPUSH to add to the left (most recent at head)
	return RedisClient.LPush(ctx, key, logJSON).Err()
}

// GetLogsFromBuffer retrieves all logs from Redis buffer
func GetLogsFromBuffer(runID, taskID uint64) ([]string, error) {
	ctx := context.Background()
	key := LogBufferPrefix + formatUint64(runID) + ":" + formatUint64(taskID)

	// Get all logs
	logs, err := RedisClient.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, err
	}

	return logs, nil
}

// ClearLogBuffer removes logs from Redis buffer after they've been flushed to MySQL
func ClearLogBuffer(runID, taskID uint64) error {
	ctx := context.Background()
	key := LogBufferPrefix + formatUint64(runID) + ":" + formatUint64(taskID)
	return RedisClient.Del(ctx, key).Err()
}

// GetActiveLogKeys returns all active log buffer keys
func GetActiveLogKeys() ([]string, error) {
	ctx := context.Background()
	pattern := LogBufferPrefix + "*"
	keys := make([]string, 0)
	var cursor uint64
	for {
		batch, nextCursor, err := RedisClient.Scan(ctx, cursor, pattern, 128).Result()
		if err != nil {
			return nil, err
		}
		keys = append(keys, batch...)
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return keys, nil
}

func formatUint64(n uint64) string {
	if n == 0 {
		return "0"
	}

	result := ""
	for n > 0 {
		result = string(rune('0'+n%10)) + result
		n /= 10
	}
	return result
}
