package excel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const (
	// StatusQueued 表示任务已入队，等待后台 worker 执行。
	StatusQueued = "queued"
	// StatusRunning 表示任务正在执行。
	StatusRunning = "running"
	// StatusSucceeded 表示任务执行成功。
	StatusSucceeded = "succeeded"
	// StatusFailed 表示任务执行失败。
	StatusFailed = "failed"
	// DateTimeLayout 表示导入导出通用时间文本格式。
	DateTimeLayout = "2006-01-02 15:04:05"
)

// JobSnapshot 表示通用导入导出任务状态快照。
type JobSnapshot struct {
	JobID             string `json:"jobId"`             // 任务 ID
	TaskID            string `json:"taskId"`            // 队列任务 ID
	Queue             string `json:"queue"`             // 队列名称
	Status            string `json:"status"`            // 任务状态
	Progress          int    `json:"progress"`          // 进度百分比
	Processed         int64  `json:"processed"`         // 已处理条数
	Total             int64  `json:"total"`             // 总条数
	EstimatedSeconds  int64  `json:"estimatedSeconds"`  // 预估剩余秒数
	FileName          string `json:"fileName"`          // 结果文件名
	DownloadReady     bool   `json:"downloadReady"`     // 是否可下载
	ErrorMessage      string `json:"errorMessage"`      // 错误信息
	CreatedAt         string `json:"createdAt"`         // 创建时间
	StartedAt         string `json:"startedAt"`         // 开始时间
	FinishedAt        string `json:"finishedAt"`        // 完成时间
	UpdatedAt         string `json:"updatedAt"`         // 最近更新时间
	FilePath          string `json:"-"`                 // 本地结果路径
	DownloadURL       string `json:"downloadUrl"`       // 下载地址
	ProcessAt         string `json:"processAt"`         // 预计执行时间
	LastProcessedAt   string `json:"lastProcessedAt"`   // 最近一批更新时间
	AverageRowsPerSec int64  `json:"averageRowsPerSec"` // 平均处理速度
	OperatorID        int    `json:"-"`                 // 发起人 ID
}

// RedisStore 表示基于 Redis 的通用任务状态存储。
type RedisStore struct {
	client     redis.UniversalClient // Redis 客户端
	keyPattern string                // 任务状态 key 模板
	ttl        time.Duration         // 任务状态保留时间
}

// ExportProgress 表示导出任务当前进度快照。
type ExportProgress struct {
	Processed         int64     // 已处理条数
	Total             int64     // 总条数
	Progress          int       // 进度百分比
	AverageRowsPerSec int64     // 平均处理速度
	EstimatedSeconds  int64     // 预估剩余秒数
	LastProcessedAt   time.Time // 最近一批处理时间
}

// NewRedisStore 创建通用任务状态 Redis 存储。
func NewRedisStore(client redis.UniversalClient, keyPattern string, ttl time.Duration) *RedisStore {
	return &RedisStore{
		client:     client,
		keyPattern: keyPattern,
		ttl:        ttl,
	}
}

// Load 把指定任务状态加载到 target。
func (s *RedisStore) Load(ctx context.Context, jobID string, target any) error {
	if s == nil || s.client == nil {
		return errors.Errorf("Redis 状态存储未初始化")
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return errors.Errorf("jobId 不能为空")
	}
	value, err := s.client.Get(ctx, fmt.Sprintf(s.keyPattern, jobID)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return gorm.ErrRecordNotFound
		}
		return errors.Wrapf(err, "imex.RedisStore.Load 读取任务[%s]失败", jobID)
	}
	if err := json.Unmarshal([]byte(value), target); err != nil {
		return errors.Wrapf(err, "imex.RedisStore.Load 解析任务[%s]失败", jobID)
	}
	return nil
}

// Save 把指定任务状态保存到 Redis。
func (s *RedisStore) Save(ctx context.Context, jobID string, value any) error {
	if s == nil || s.client == nil {
		return errors.Errorf("Redis 状态存储未初始化")
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return errors.Errorf("jobId 不能为空")
	}
	body, err := json.Marshal(value)
	if err != nil {
		return errors.Wrapf(err, "imex.RedisStore.Save 序列化任务[%s]失败", jobID)
	}
	if err := s.client.Set(ctx, fmt.Sprintf(s.keyPattern, jobID), body, s.ttl).Err(); err != nil {
		return errors.Wrapf(err, "imex.RedisStore.Save 保存任务[%s]失败", jobID)
	}
	return nil
}

// FormatDateTime 把时间格式化为标准文本；零值时间返回空字符串。
func FormatDateTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(DateTimeLayout)
}

// ParseDateTime 解析标准时间文本。
func ParseDateTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	return time.ParseInLocation(DateTimeLayout, value, time.Local)
}

// ClampProgress 保证进度始终落在 0-100 范围内。
func ClampProgress(progress int) int {
	if progress < 0 {
		return 0
	}
	if progress > 100 {
		return 100
	}
	return progress
}

// BuildMetrics 计算进度、平均速度和剩余时间预估。
func BuildMetrics(total int64, processed int64, startedAt time.Time, now time.Time) (int, int64, int64) {
	if processed <= 0 {
		return 0, 0, 0
	}
	elapsedSeconds := int64(now.Sub(startedAt).Seconds())
	if elapsedSeconds <= 0 {
		elapsedSeconds = 1
	}
	averageRowsPerSec := processed / elapsedSeconds
	if averageRowsPerSec <= 0 {
		averageRowsPerSec = 1
	}
	progress := 100
	estimatedSeconds := int64(0)
	if total > 0 {
		progress = int(processed * 100 / total)
		if progress > 100 {
			progress = 100
		}
		remainingRows := total - processed
		if remainingRows > 0 {
			estimatedSeconds = (remainingRows + averageRowsPerSec - 1) / averageRowsPerSec
		}
	}
	return progress, averageRowsPerSec, estimatedSeconds
}
