package audit

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"admin/internal/config"
	"admin/internal/infra/collectorx"
	"admin/internal/model"

	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

const (
	// AdminLogCollectorBizType 表示 admin_log 批量入库的 Collector 业务类型。
	AdminLogCollectorBizType = config.CollectorBizTypeAdminLogAudit
	adminLogPartitionKey     = "admin_log" // 审计日志固定分区键，保证同任务入库顺序稳定
)

// CollectorEnqueuer 约束通用 Collector 的最小投递能力。
type CollectorEnqueuer interface {
	Enqueue(context.Context, collectorx.Event) (string, error)
}

// CollectorWriter 将审计日志投递到 Collector Kafka 正常链路。
type CollectorWriter struct {
	collector CollectorEnqueuer // Collector 投递入口
}

// NewCollectorWriter 创建审计日志 Collector 写入器。
func NewCollectorWriter(collector CollectorEnqueuer) *CollectorWriter {
	return &CollectorWriter{collector: collector}
}

// EnqueueAdminLog 把单条审计日志封装成 Collector 事件。
func (w *CollectorWriter) EnqueueAdminLog(ctx context.Context, eventID string, row model.AdminLog) error {
	if w == nil || w.collector == nil {
		return errors.Errorf("审计日志 Collector 写入器未初始化")
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return errors.Errorf("审计日志缺少 event_id")
	}
	row.EventID = eventID
	payload, err := json.Marshal(row)
	if err != nil {
		return errors.Wrap(err, "编码审计日志 Collector 事件失败")
	}
	_, err = w.collector.Enqueue(ctx, collectorx.Event{
		EventID:      eventID,
		BizType:      AdminLogCollectorBizType,
		PartitionKey: adminLogPartitionKey,
		Payload:      payload,
	})
	return errors.Tag(err)
}

// AdminLogBatchProcessor 批量消费审计日志 Collector 事件并写入 admin_log。
type AdminLogBatchProcessor struct {
	db *gorm.DB // admin_log 写库连接
}

// NewAdminLogBatchProcessor 创建 admin_log 批量写入处理器。
func NewAdminLogBatchProcessor(db *gorm.DB) *AdminLogBatchProcessor {
	return &AdminLogBatchProcessor{db: db}
}

// ProcessBatch 批量写入 admin_log；空批次直接返回，避免无意义 DB 调用。
func (p *AdminLogBatchProcessor) ProcessBatch(ctx context.Context, events []collectorx.Event) ([]collectorx.ProcessResult, error) {
	if len(events) == 0 {
		return nil, nil
	}
	if p == nil || p.db == nil {
		return nil, errors.Errorf("admin_log 批量 Processor 缺少数据库连接")
	}

	now := time.Now()
	rows := make([]model.AdminLog, 0, len(events))
	results := make([]collectorx.ProcessResult, 0, len(events))
	rowEventIDs := make([]string, 0, len(events))
	seenEventIDs := make(map[string]struct{}, len(events))
	for _, event := range events {
		eventID := strings.TrimSpace(event.EventID)
		if eventID == "" {
			results = append(results, collectorx.ProcessResult{EventID: event.EventID, Success: false, Error: "admin_log 审计事件缺少 event_id"})
			continue
		}
		if _, ok := seenEventIDs[eventID]; ok {
			continue
		}
		seenEventIDs[eventID] = struct{}{}
		var row model.AdminLog
		if err := json.Unmarshal(event.Payload, &row); err != nil {
			results = append(results, collectorx.ProcessResult{EventID: eventID, Success: false, Error: "解析 admin_log 审计事件失败"})
			continue
		}
		// 主键只能由目标库生成，禁止重放载荷中的旧 ID 触发错误的主键幂等命中。
		row.ID = 0
		row.EventID = eventID
		if row.CreatedAt.IsZero() {
			row.CreatedAt = now
		}
		rows = append(rows, row)
		rowEventIDs = append(rowEventIDs, eventID)
	}
	if len(rows) > 0 {
		if err := model.CreateAdminLogs(p.db.WithContext(ctx).Clauses(dbresolver.Write), rows); err != nil {
			return nil, errors.Wrap(err, "批量写入 admin_log 审计日志失败")
		}
		for _, eventID := range rowEventIDs {
			results = append(results, collectorx.ProcessResult{EventID: eventID, Success: true})
		}
	}
	return results, nil
}
