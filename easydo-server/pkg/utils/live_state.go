package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	liveTaskStateKeyPrefix = "easydo:live:task:"
	liveRunStateKeyPrefix  = "easydo:live:run:"
	liveTaskSeqKeyPrefix   = "easydo:live:task-seq:"
	liveRunSeqKeyPrefix    = "easydo:live:run-seq:"
	liveRunTaskIndexPrefix = "easydo:live:run-tasks:"
)

type LiveTaskState struct {
	TaskID          uint64 `json:"task_id"`
	RunID           uint64 `json:"run_id"`
	NodeID          string `json:"node_id,omitempty"`
	Status          string `json:"status"`
	Seq             int64  `json:"seq"`
	OwnerServerID   string `json:"owner_server_id,omitempty"`
	AgentSessionID  string `json:"agent_session_id,omitempty"`
	DispatchAttempt int    `json:"dispatch_attempt,omitempty"`
	RetryCount      int    `json:"retry_count,omitempty"`
	StartTime       int64  `json:"start_time,omitempty"`
	EndTime         int64  `json:"end_time,omitempty"`
	Duration        int    `json:"duration,omitempty"`
	ExitCode        int    `json:"exit_code,omitempty"`
	ErrorMsg        string `json:"error_msg,omitempty"`
	AgentName       string `json:"agent_name,omitempty"`
	UpdatedAt       int64  `json:"updated_at"`
	IsTerminal      bool   `json:"is_terminal"`
}

type LiveRunState struct {
	RunID         uint64 `json:"run_id"`
	Status        string `json:"status"`
	Seq           int64  `json:"seq"`
	OwnerServerID string `json:"owner_server_id,omitempty"`
	Duration      int    `json:"duration,omitempty"`
	ErrorMsg      string `json:"error_msg,omitempty"`
	UpdatedAt     int64  `json:"updated_at"`
	IsTerminal    bool   `json:"is_terminal"`
}

func liveTaskStateKey(taskID uint64) string {
	return liveTaskStateKeyPrefix + formatUint64(taskID)
}

func liveRunStateKey(runID uint64) string {
	return liveRunStateKeyPrefix + formatUint64(runID)
}

func liveTaskSeqKey(taskID uint64) string {
	return liveTaskSeqKeyPrefix + formatUint64(taskID)
}

func liveRunSeqKey(runID uint64) string {
	return liveRunSeqKeyPrefix + formatUint64(runID)
}

func liveRunTaskIndexKey(runID uint64) string {
	return liveRunTaskIndexPrefix + formatUint64(runID)
}

func liveStateTTL() time.Duration {
	return 90 * time.Second
}

func liveStateTerminalGraceTTL() time.Duration {
	return 120 * time.Second
}

func liveStateSeqTTL() time.Duration {
	return 24 * time.Hour
}

func NextLiveTaskSeq(ctx context.Context, taskID uint64) (int64, error) {
	if RedisClient == nil {
		return 0, fmt.Errorf("redis client not initialized")
	}
	seq, err := RedisClient.Incr(ctx, liveTaskSeqKey(taskID)).Result()
	if err != nil {
		return 0, err
	}
	_ = RedisClient.Expire(ctx, liveTaskSeqKey(taskID), liveStateSeqTTL()).Err()
	return seq, nil
}

func NextLiveRunSeq(ctx context.Context, runID uint64) (int64, error) {
	if RedisClient == nil {
		return 0, fmt.Errorf("redis client not initialized")
	}
	seq, err := RedisClient.Incr(ctx, liveRunSeqKey(runID)).Result()
	if err != nil {
		return 0, err
	}
	_ = RedisClient.Expire(ctx, liveRunSeqKey(runID), liveStateSeqTTL()).Err()
	return seq, nil
}

func SaveLiveTaskState(ctx context.Context, state LiveTaskState, terminal bool) error {
	if RedisClient == nil {
		return fmt.Errorf("redis client not initialized")
	}
	if state.TaskID == 0 || state.RunID == 0 || state.Status == "" || state.Seq <= 0 {
		return fmt.Errorf("invalid live task state")
	}
	if state.UpdatedAt == 0 {
		state.UpdatedAt = time.Now().Unix()
	}
	state.IsTerminal = terminal
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	ttl := liveStateTTL()
	if terminal {
		ttl = liveStateTerminalGraceTTL()
	}
	pipe := RedisClient.TxPipeline()
	pipe.Set(ctx, liveTaskStateKey(state.TaskID), data, ttl)
	pipe.SAdd(ctx, liveRunTaskIndexKey(state.RunID), state.TaskID)
	pipe.Expire(ctx, liveRunTaskIndexKey(state.RunID), ttl)
	_, err = pipe.Exec(ctx)
	return err
}

func GetLiveTaskState(ctx context.Context, taskID uint64) (*LiveTaskState, error) {
	if RedisClient == nil {
		return nil, fmt.Errorf("redis client not initialized")
	}
	if taskID == 0 {
		return nil, nil
	}
	data, err := RedisClient.Get(ctx, liveTaskStateKey(taskID)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}
	var state LiveTaskState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func GetLiveTaskStatesForRun(ctx context.Context, runID uint64) ([]LiveTaskState, bool, error) {
	if RedisClient == nil {
		return nil, false, fmt.Errorf("redis client not initialized")
	}
	if runID == 0 {
		return nil, false, nil
	}
	ids, err := RedisClient.SMembers(ctx, liveRunTaskIndexKey(runID)).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, false, nil
		}
		return nil, false, err
	}
	if len(ids) == 0 {
		return nil, false, nil
	}
	keys := make([]string, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, liveTaskStateKey(toUint64(id)))
	}
	values, err := RedisClient.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, false, err
	}
	states := make([]LiveTaskState, 0, len(values))
	complete := true
	for _, raw := range values {
		if raw == nil {
			complete = false
			continue
		}
		var state LiveTaskState
		if err := json.Unmarshal([]byte(fmt.Sprintf("%v", raw)), &state); err != nil {
			complete = false
			continue
		}
		states = append(states, state)
	}
	return states, complete, nil
}

func SaveLiveRunState(ctx context.Context, state LiveRunState, terminal bool) error {
	if RedisClient == nil {
		return fmt.Errorf("redis client not initialized")
	}
	if state.RunID == 0 || state.Status == "" || state.Seq <= 0 {
		return fmt.Errorf("invalid live run state")
	}
	if state.UpdatedAt == 0 {
		state.UpdatedAt = time.Now().Unix()
	}
	state.IsTerminal = terminal
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	ttl := liveStateTTL()
	if terminal {
		ttl = liveStateTerminalGraceTTL()
	}
	return RedisClient.Set(ctx, liveRunStateKey(state.RunID), data, ttl).Err()
}

func GetLiveRunState(ctx context.Context, runID uint64) (*LiveRunState, error) {
	if RedisClient == nil {
		return nil, fmt.Errorf("redis client not initialized")
	}
	if runID == 0 {
		return nil, nil
	}
	data, err := RedisClient.Get(ctx, liveRunStateKey(runID)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}
	var state LiveRunState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}
