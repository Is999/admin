package queryplan

import (
	"fmt"
	"strings"
	"time"

	"admin/internal/jobs/usertag/route"

	"github.com/Is999/go-utils/errors"
)

// TimeWindow 表示查询时间窗口。
type TimeWindow struct {
	Start time.Time // 开始时间，包含
	End   time.Time // 结束时间，不包含
}

// Empty 判断时间窗口是否为空。
func (w TimeWindow) Empty() bool {
	return w.Start.IsZero() || w.End.IsZero() || !w.Start.Before(w.End)
}

// UIDSet 表示批次级 UID 集合。
// full 场景不能把全量 UID 放入内存，只允许用 AllowFullShardScan 配合游标扫描。
type UIDSet struct {
	UIDs []int64 // 当前批次 UID 列表
}

// Empty 判断 UID 集合是否为空。
func (s UIDSet) Empty() bool {
	return len(s.UIDs) == 0
}

// Aggregate 描述一个下推到数据源侧执行的聚合目标。
type Aggregate struct {
	Name string // 聚合名称
	Expr string // 聚合表达式
}

// Plan 描述一次可执行的数据源查询计划。
type Plan struct {
	Name               string      // 查询计划名称
	Source             string      // 数据源类型：mysql/clickhouse/redis/event_outbox
	Table              string      // 查询表名或逻辑源名
	Fields             []string    // 需要读取的字段清单，禁止使用 *
	Aggregates         []Aggregate // 聚合目标清单
	Shard              route.Shard // 当前分片
	Window             TimeWindow  // 时间窗口
	UIDs               UIDSet      // 批次 UID 集合
	AllowFullShardScan bool        // 是否允许分片内游标扫描
	BatchSize          int         // 查询批次大小
}

// Validate 校验查询计划是否安全，默认拒绝无条件全表扫描。
func (p Plan) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.Errorf("查询计划名称不能为空")
	}
	if strings.TrimSpace(p.Source) == "" {
		return errors.Errorf("查询计划数据源不能为空 name=%s", p.Name)
	}
	if strings.TrimSpace(p.Table) == "" {
		return errors.Errorf("查询计划表名不能为空 name=%s", p.Name)
	}
	for _, field := range p.Fields {
		if strings.TrimSpace(field) == "*" {
			return errors.Errorf("查询计划禁止 SELECT * name=%s", p.Name)
		}
	}
	hasUIDs := !p.UIDs.Empty()
	hasWindow := !p.Window.Empty()
	hasShard := p.Shard.Total > 0
	if !p.AllowFullShardScan && !hasUIDs && !hasWindow {
		return errors.Errorf("查询计划缺少 UID 或时间窗口，默认拒绝全表扫描 name=%s", p.Name)
	}
	if p.AllowFullShardScan && !hasShard {
		return errors.Errorf("分片扫描必须携带 shard 信息 name=%s", p.Name)
	}
	if p.BatchSize < 0 {
		return errors.Errorf("查询批次大小不能为负数 name=%s", p.Name)
	}
	return nil
}

// CacheKey 返回批次级复用键。
// 该键只允许在同一 workflow/node/shard/batch 内使用，不能跨工作流或长时间缓存。
func (p Plan) CacheKey() string {
	return fmt.Sprintf("%s|%s|%s|%d/%d|%s|%s|%d",
		p.Source, p.Table, p.Name, p.Shard.Index, p.Shard.Total,
		p.Window.Start.Format(time.RFC3339Nano), p.Window.End.Format(time.RFC3339Nano), p.BatchSize)
}
