package handlers

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"easydo-server/internal/config"
	"easydo-server/internal/models"
	"easydo-server/pkg/storage"
)

type fileLogEntry struct {
	AgentID       uint64 `json:"agent_id,omitempty"`
	TaskID        uint64 `json:"task_id"`
	PipelineRunID uint64 `json:"pipeline_run_id"`
	Level         string `json:"level"`
	Message       string `json:"message"`
	Source        string `json:"source"`
	Timestamp     int64  `json:"timestamp"`
	LineNumber    int    `json:"line_number,omitempty"`
	Attempt       int    `json:"attempt,omitempty"`
	Seq           int64  `json:"seq,omitempty"`
}

type liveLogQuery struct {
	TaskID   uint64
	RunID    uint64
	Attempt  int
	SinceSeq int64
	Level    string
	Source   string
}

type liveTaskBuffer struct {
	TaskID        uint64
	PipelineRunID uint64
	Attempt       int
	SegmentNo     int
	Entries       []fileLogEntry
	Completed     bool
	LastFlushTime int64
}

type taskLogStore struct {
	mu          sync.RWMutex
	buffers     map[string]*liveTaskBuffer
	objectStore storage.ObjectStore
	bucket      string
	storeOnce   sync.Once
}

func newTaskLogStore() *taskLogStore {
	return &taskLogStore{buffers: make(map[string]*liveTaskBuffer)}
}

func (s *taskLogStore) bufferKey(taskID uint64, attempt int) string {
	return fmt.Sprintf("%d:%d", taskID, attempt)
}

// Append adds one live chunk to the in-process buffer for a `(task, attempt)`.
//
// This buffer is only a low-latency serving / batching layer. Durability does
// not depend on it: the websocket ingest path persists every chunk into
// `agent_log_chunks` before Append is called. That separation is what lets log
// reads recover missing lines after owner failover even if the old owner's memory
// buffer died before a segment flush completed.
func (s *taskLogStore) Append(entry fileLogEntry) error {
	if entry.PipelineRunID == 0 || entry.TaskID == 0 || entry.Attempt <= 0 {
		return fmt.Errorf("invalid log entry")
	}
	if entry.Timestamp == 0 {
		entry.Timestamp = time.Now().Unix()
	}
	if entry.Source == "" {
		entry.Source = "stdout"
	}
	if entry.Level == "" {
		entry.Level = "info"
	}

	key := s.bufferKey(entry.TaskID, entry.Attempt)
	s.mu.Lock()
	buffer, ok := s.buffers[key]
	if !ok {
		buffer = &liveTaskBuffer{TaskID: entry.TaskID, PipelineRunID: entry.PipelineRunID, Attempt: entry.Attempt, SegmentNo: 1, LastFlushTime: time.Now().Unix()}
		s.buffers[key] = buffer
	}
	buffer.Entries = append(buffer.Entries, entry)
	flushNow := len(buffer.Entries) >= logSegmentMaxLines()
	if !flushNow {
		ageSeconds := time.Now().Unix() - buffer.LastFlushTime
		flushNow = ageSeconds >= int64(logSegmentMaxAgeSeconds())
	}
	s.mu.Unlock()
	if flushNow {
		return s.flushSegment(context.Background(), entry.TaskID, entry.Attempt, false)
	}
	return nil
}

func (s *taskLogStore) FinishTask(taskID uint64, attempt int) error {
	if taskID == 0 || attempt <= 0 {
		return nil
	}
	return s.flushSegment(context.Background(), taskID, attempt, true)
}

// QueryTaskLogs reconstructs task history from every available storage layer.
//
// The merge order is intentional:
// 1. object-storage segments (best long-term source),
// 2. current-process live buffer for in-flight tail reads.
func (s *taskLogStore) QueryTaskLogs(runID uint64, taskID uint64, level string) ([]models.AgentLog, error) {
	entries, err := s.readSegments(context.Background(), taskID, runID, 0)
	if err != nil {
		return nil, err
	}
	entries = append(entries, s.queryLive(liveLogQuery{TaskID: taskID, RunID: runID, Level: level})...)
	return filterEntries(dedupeEntries(entries), taskID, level, ""), nil
}

// QueryRunLogs applies the same reconstruction strategy as QueryTaskLogs, but
// across all tasks that belong to the run.
func (s *taskLogStore) QueryRunLogs(runID uint64, taskID uint64, level, source string) ([]models.AgentLog, error) {
	var segments []models.AgentLogSegment
	query := models.DB.Where("pipeline_run_id = ?", runID)
	if taskID > 0 {
		query = query.Where("task_id = ?", taskID)
	}
	if err := query.Order("created_at ASC").Find(&segments).Error; err != nil {
		return nil, err
	}
	entries := make([]fileLogEntry, 0, len(segments)*8)
	for _, segment := range segments {
		segmentEntries, err := s.readSegmentObject(context.Background(), segment)
		if err != nil {
			return nil, err
		}
		entries = append(entries, segmentEntries...)
	}
	entries = append(entries, s.queryLive(liveLogQuery{TaskID: taskID, RunID: runID, Level: level, Source: source})...)
	return filterEntries(dedupeEntries(entries), taskID, level, source), nil
}

func (s *taskLogStore) QueryLiveTaskLogs(taskID uint64, attempt int, sinceSeq int64) ([]fileLogEntry, error) {
	if taskID == 0 || attempt <= 0 {
		return []fileLogEntry{}, nil
	}
	entries := s.queryLive(liveLogQuery{TaskID: taskID, Attempt: attempt, SinceSeq: sinceSeq})
	sortEntries(entries)
	return entries, nil
}

func (s *taskLogStore) readSegments(ctx context.Context, taskID, runID uint64, attempt int) ([]fileLogEntry, error) {
	query := models.DB.Model(&models.AgentLogSegment{})
	if taskID > 0 {
		query = query.Where("task_id = ?", taskID)
	}
	if runID > 0 {
		query = query.Where("pipeline_run_id = ?", runID)
	}
	if attempt > 0 {
		query = query.Where("attempt = ?", attempt)
	}
	var segments []models.AgentLogSegment
	if err := query.Order("created_at ASC").Find(&segments).Error; err != nil {
		return nil, err
	}
	entries := make([]fileLogEntry, 0, len(segments)*8)
	for _, segment := range segments {
		segmentEntries, err := s.readSegmentObject(ctx, segment)
		if err != nil {
			return nil, err
		}
		entries = append(entries, segmentEntries...)
	}
	return entries, nil
}

func (s *taskLogStore) queryLive(q liveLogQuery) []fileLogEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries := make([]fileLogEntry, 0, 64)
	for _, buffer := range s.buffers {
		if q.TaskID > 0 && buffer.TaskID != q.TaskID {
			continue
		}
		if q.RunID > 0 && buffer.PipelineRunID != q.RunID {
			continue
		}
		if q.Attempt > 0 && buffer.Attempt != q.Attempt {
			continue
		}
		for _, entry := range buffer.Entries {
			if q.SinceSeq > 0 && entry.Seq <= q.SinceSeq {
				continue
			}
			if q.Level != "" && !strings.EqualFold(entry.Level, q.Level) {
				continue
			}
			if q.Source != "" && !strings.EqualFold(entry.Source, q.Source) {
				continue
			}
			entries = append(entries, entry)
		}
	}
	return entries
}

// flushSegment seals the current in-memory slice for one `(task, attempt)` into
// a compact segment object plus MySQL segment index row.
//
// The lock is only held long enough to snapshot and reset the mutable buffer.
// Compression, object upload, and DB writes happen outside the lock so log
// producers are not blocked by slower storage work.
func (s *taskLogStore) flushSegment(ctx context.Context, taskID uint64, attempt int, completed bool) error {
	key := s.bufferKey(taskID, attempt)
	s.mu.Lock()
	buffer, ok := s.buffers[key]
	if !ok || len(buffer.Entries) == 0 {
		if ok && completed {
			buffer.Completed = true
			delete(s.buffers, key)
		}
		s.mu.Unlock()
		return nil
	}
	entries := append([]fileLogEntry(nil), buffer.Entries...)
	segmentNo := buffer.SegmentNo
	runID := buffer.PipelineRunID
	buffer.Entries = nil
	buffer.SegmentNo++
	buffer.Completed = completed
	buffer.LastFlushTime = time.Now().Unix()
	if completed {
		defer delete(s.buffers, key)
	}
	s.mu.Unlock()
	sortEntries(entries)
	body, checksum, err := marshalCompressed(entries)
	if err != nil {
		return err
	}
	objectKey := buildLogObjectKey(runID, taskID, attempt, segmentNo)
	objectSize := int64(len(body))
	s.ensureObjectStore()
	if s.objectStore != nil {
		if err := s.objectStore.EnsureBucket(ctx); err != nil {
			return err
		}
		storedSize, err := s.objectStore.PutObject(ctx, objectKey, body, "application/gzip")
		if err != nil {
			return err
		}
		objectSize = storedSize
	}
	segment := &models.AgentLogSegment{
		TaskID:        taskID,
		PipelineRunID: runID,
		AgentID:       entries[0].AgentID,
		Attempt:       attempt,
		SegmentNo:     segmentNo,
		StartSeq:      entries[0].Seq,
		EndSeq:        entries[len(entries)-1].Seq,
		LineCount:     len(entries),
		ObjectKey:     objectKey,
		ObjectBucket:  s.bucket,
		ObjectSize:    objectSize,
		ContentType:   "application/gzip",
		Checksum:      checksum,
		Completed:     completed,
	}
	return models.DB.Create(segment).Error
}

func (s *taskLogStore) readSegmentObject(ctx context.Context, segment models.AgentLogSegment) ([]fileLogEntry, error) {
	s.ensureObjectStore()
	if s.objectStore == nil {
		return []fileLogEntry{}, nil
	}
	reader, err := s.objectStore.GetObject(ctx, segment.ObjectKey)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	gzReader, err := gzip.NewReader(reader)
	if err != nil {
		return nil, err
	}
	defer gzReader.Close()
	data, err := io.ReadAll(gzReader)
	if err != nil {
		return nil, err
	}
	var entries []fileLogEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func filterEntries(entries []fileLogEntry, taskID uint64, level, source string) []models.AgentLog {
	sortEntries(entries)
	result := make([]models.AgentLog, 0, len(entries))
	for _, e := range entries {
		if taskID > 0 && e.TaskID != taskID {
			continue
		}
		if level != "" && !strings.EqualFold(e.Level, level) {
			continue
		}
		if source != "" && !strings.EqualFold(e.Source, source) {
			continue
		}
		result = append(result, models.AgentLog{TaskID: e.TaskID, PipelineRunID: e.PipelineRunID, Level: e.Level, Message: e.Message, Timestamp: e.Timestamp, Source: e.Source})
	}
	return result
}

// mergeEntries concatenates multiple storage layers before we perform global
// dedupe/order normalization.
func mergeEntries(entries ...[]fileLogEntry) []fileLogEntry {
	merged := make([]fileLogEntry, 0)
	for _, part := range entries {
		merged = append(merged, part...)
	}
	return merged
}

// dedupeEntries removes overlapping logical lines produced by multi-layer log
// reconstruction.
//
// Overlap is expected: the same line may exist in a finished segment, in the
// durable chunk table, and in the current live buffer during flush boundaries or
// replay windows. We prefer storing extra copies over risking loss, then make
// reads canonical here.
func dedupeEntries(entries []fileLogEntry) []fileLogEntry {
	if len(entries) <= 1 {
		return entries
	}
	seen := make(map[string]struct{}, len(entries))
	result := make([]fileLogEntry, 0, len(entries))
	for _, entry := range entries {
		key := fmt.Sprintf("%d:%d:%d:%s", entry.TaskID, entry.Attempt, entry.Seq, entry.Message)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, entry)
	}
	return result
}

func sortEntries(entries []fileLogEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Timestamp == entries[j].Timestamp {
			if entries[i].Seq == entries[j].Seq {
				return entries[i].LineNumber < entries[j].LineNumber
			}
			return entries[i].Seq < entries[j].Seq
		}
		return entries[i].Timestamp < entries[j].Timestamp
	})
}

func marshalCompressed(entries []fileLogEntry) ([]byte, string, error) {
	raw, err := json.Marshal(entries)
	if err != nil {
		return nil, "", err
	}
	hash := sha256.Sum256(raw)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(raw); err != nil {
		return nil, "", err
	}
	if err := gz.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), hex.EncodeToString(hash[:]), nil
}

func buildLogObjectKey(runID, taskID uint64, attempt, segmentNo int) string {
	return fmt.Sprintf("runs/%d/tasks/%d/attempts/%d/segments/%06d.json.gz", runID, taskID, attempt, segmentNo)
}

func logSegmentMaxLines() int {
	if config.Config == nil {
		return 200
	}
	v := config.Config.GetInt("logging.segment_max_lines")
	if v <= 0 {
		return 200
	}
	return v
}

func logSegmentMaxAgeSeconds() int {
	if config.Config == nil {
		return 60
	}
	v := config.Config.GetInt("logging.segment_max_age_seconds")
	if v <= 0 {
		return 60
	}
	return v
}

func (s *taskLogStore) ensureObjectStore() {
	s.storeOnce.Do(func() {
		if config.Config == nil {
			return
		}
		objStore, err := storage.NewObjectStore()
		if err != nil {
			return
		}
		s.objectStore = objStore
		s.bucket = objStore.Bucket()
	})
}

var agentFileLogs = newTaskLogStore()
