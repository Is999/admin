package taskstats

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// ActionRead 表示读取或扫描数据。
	ActionRead = "read"
	// ActionInsert 表示新增数据。
	ActionInsert = "insert"
	// ActionUpdate 表示更新数据。
	ActionUpdate = "update"
	// ActionDelete 表示删除数据。
	ActionDelete = "delete"
	// ActionUpsert 表示新增或更新数据。
	ActionUpsert = "upsert"
	// ActionSkip 表示跳过或未命中数据。
	ActionSkip = "skip"
	// ActionError 表示处理过程中隔离的错误数据。
	ActionError = "error"
	// ActionCustom 表示非标准动作类型。
	ActionCustom = "custom"
	// DetailNameDefault 表示未传业务对象名时的默认明细名称。
	DetailNameDefault = "default"
	// DetailNameSeparator 表示处理量明细名称的层级分隔符。
	DetailNameSeparator = "."
	// DetailPartLock 表示互斥锁跳过明细。
	DetailPartLock = "lock"
	// DetailPartRows 表示通用行数明细。
	DetailPartRows = "rows"
	// DetailPartSkipped 表示任务主动跳过明细。
	DetailPartSkipped = "skipped"
	// DetailPartUIDs 表示 UID 数量明细。
	DetailPartUIDs = "uids"
	// DetailPartBizDates 表示业务日期数量明细。
	DetailPartBizDates = "biz_dates"
)

// trackerKey 是任务执行统计器在 context 中的私有 key。
type trackerKey struct{}

// Detail 表示单个动作和对象维度的处理量明细。
type Detail struct {
	Action    string `json:"action"`              // 动作类型，如 read/insert/update/delete
	Name      string `json:"name"`                // 业务对象或阶段名称
	Count     int64  `json:"count"`               // 本维度累计处理数量
	Times     int64  `json:"times"`               // 本维度记录次数
	ElapsedMS int64  `json:"elapsedMs,omitempty"` // 本维度累计耗时，毫秒
}

// Snapshot 是任务执行统计器的只读快照。
type Snapshot struct {
	Name        string   `json:"name,omitempty"`        // 统计器名称，通常是任务名或工作流节点名
	StartedAt   string   `json:"startedAt,omitempty"`   // 统计开始时间
	FinishedAt  string   `json:"finishedAt,omitempty"`  // 快照生成时间
	DurationMS  int64    `json:"durationMs,omitempty"`  // 统计总耗时，毫秒
	TotalCount  int64    `json:"totalCount,omitempty"`  // 所有动作累计数量
	ReadCount   int64    `json:"readCount,omitempty"`   // 读取数量
	InsertCount int64    `json:"insertCount,omitempty"` // 新增数量
	UpdateCount int64    `json:"updateCount,omitempty"` // 更新数量
	DeleteCount int64    `json:"deleteCount,omitempty"` // 删除数量
	UpsertCount int64    `json:"upsertCount,omitempty"` // 新增或更新数量
	SkipCount   int64    `json:"skipCount,omitempty"`   // 跳过数量
	ErrorCount  int64    `json:"errorCount,omitempty"`  // 隔离错误数量
	Details     []Detail `json:"details,omitempty"`     // 按动作和对象聚合后的明细
}

// MergeSnapshots 把多个执行快照合并为一个聚合快照。
func MergeSnapshots(name string, snapshots ...*Snapshot) *Snapshot {
	details := make(map[string]*Detail)
	merged := Snapshot{
		Name:    strings.TrimSpace(name),
		Details: make([]Detail, 0),
	}
	var (
		startedAt  time.Time
		finishedAt time.Time
		hasStarted bool
		hasFinish  bool
		fallbackMS int64
	)
	for _, snapshot := range snapshots {
		if snapshot == nil || snapshot.Empty() {
			continue
		}
		merged.TotalCount += snapshot.TotalCount
		merged.ReadCount += snapshot.ReadCount
		merged.InsertCount += snapshot.InsertCount
		merged.UpdateCount += snapshot.UpdateCount
		merged.DeleteCount += snapshot.DeleteCount
		merged.UpsertCount += snapshot.UpsertCount
		merged.SkipCount += snapshot.SkipCount
		merged.ErrorCount += snapshot.ErrorCount
		fallbackMS += snapshot.DurationMS
		if parsedStartedAt, ok := parseSnapshotTime(snapshot.StartedAt); ok && (!hasStarted || parsedStartedAt.Before(startedAt)) {
			startedAt = parsedStartedAt
			hasStarted = true
		}
		if parsedFinishedAt, ok := parseSnapshotTime(snapshot.FinishedAt); ok && (!hasFinish || parsedFinishedAt.After(finishedAt)) {
			finishedAt = parsedFinishedAt
			hasFinish = true
		}
		for _, detail := range snapshot.Details {
			mergeDetail(details, detail)
		}
	}
	if merged.TotalCount == 0 && len(details) == 0 {
		return nil
	}
	for _, detail := range details {
		if detail == nil {
			continue
		}
		merged.Details = append(merged.Details, *detail)
	}
	sortDetails(merged.Details)
	if hasStarted {
		merged.StartedAt = startedAt.Format(time.RFC3339)
	}
	if hasFinish {
		merged.FinishedAt = finishedAt.Format(time.RFC3339)
	}
	if hasStarted && hasFinish && finishedAt.After(startedAt) {
		merged.DurationMS = durationMilliseconds(finishedAt.Sub(startedAt))
	} else {
		merged.DurationMS = fallbackMS
	}
	return &merged
}

// Empty 判断快照是否没有任何业务处理量。
func (s Snapshot) Empty() bool {
	return s.TotalCount == 0 && len(s.Details) == 0
}

// Tracker 在单个任务 context 内累计处理量和耗时。
type Tracker struct {
	mu        sync.Mutex         // 保护 details 累计状态
	name      string             // 当前统计器名称
	startedAt time.Time          // 统计开始时间
	details   map[string]*Detail // 按业务动作聚合的处理明细
}

// New 创建新的执行统计器。
func New(name string) *Tracker {
	return &Tracker{
		name:      strings.TrimSpace(name),
		startedAt: time.Now(),
		details:   make(map[string]*Detail),
	}
}

// WithTracker 把执行统计器挂到 context 中。
func WithTracker(ctx context.Context, name string) (context.Context, *Tracker) {
	if ctx == nil {
		ctx = context.Background()
	}
	if tracker := FromContext(ctx); tracker != nil {
		return ctx, tracker
	}
	tracker := New(name)
	return context.WithValue(ctx, trackerKey{}, tracker), tracker
}

// FromContext 读取当前任务执行统计器。
func FromContext(ctx context.Context) *Tracker {
	if ctx == nil {
		return nil
	}
	if tracker, ok := ctx.Value(trackerKey{}).(*Tracker); ok {
		return tracker
	}
	return nil
}

// Record 追加一次处理量记录。
func Record(ctx context.Context, action string, name string, count int64) {
	Observe(ctx, action, name, count, 0)
}

// Observe 追加一次处理量和耗时记录。
func Observe(ctx context.Context, action string, name string, count int64, elapsed time.Duration) {
	if tracker := FromContext(ctx); tracker != nil {
		tracker.Record(action, name, count, elapsed)
	}
}

// RecordRead 记录读取数量。
func RecordRead(ctx context.Context, name string, count int64) {
	Record(ctx, ActionRead, name, count)
}

// RecordInsert 记录新增数量。
func RecordInsert(ctx context.Context, name string, count int64) {
	Record(ctx, ActionInsert, name, count)
}

// RecordUpdate 记录更新数量。
func RecordUpdate(ctx context.Context, name string, count int64) {
	Record(ctx, ActionUpdate, name, count)
}

// RecordDelete 记录删除数量。
func RecordDelete(ctx context.Context, name string, count int64) {
	Record(ctx, ActionDelete, name, count)
}

// RecordUpsert 记录新增或更新数量。
func RecordUpsert(ctx context.Context, name string, count int64) {
	Record(ctx, ActionUpsert, name, count)
}

// RecordSkip 记录跳过数量。
func RecordSkip(ctx context.Context, name string, count int64) {
	Record(ctx, ActionSkip, name, count)
}

// RecordError 记录隔离错误数量。
func RecordError(ctx context.Context, name string, count int64) {
	Record(ctx, ActionError, name, count)
}

// JoinDetailName 拼接稳定处理量明细名称，空片段会被忽略。
func JoinDetailName(parts ...string) string {
	names := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(strings.TrimSpace(part), DetailNameSeparator)
		if part == "" {
			continue
		}
		names = append(names, part)
	}
	if len(names) == 0 {
		return DetailNameDefault
	}
	return strings.Join(names, DetailNameSeparator)
}

// Record 追加一次处理量和耗时记录。
func (t *Tracker) Record(action string, name string, count int64, elapsed time.Duration) {
	if t == nil {
		return
	}
	action = normalizeAction(action)
	name = strings.TrimSpace(name)
	if name == "" {
		name = DetailNameDefault
	}
	if count < 0 {
		count = 0
	}
	elapsedMS := durationMilliseconds(elapsed)
	t.mu.Lock()
	defer t.mu.Unlock()
	key := action + "\x00" + name
	detail := t.details[key]
	if detail == nil {
		detail = &Detail{Action: action, Name: name}
		t.details[key] = detail
	}
	detail.Count += count
	detail.Times++
	detail.ElapsedMS += elapsedMS
}

// Snapshot 返回当前统计器的聚合快照。
func (t *Tracker) Snapshot() Snapshot {
	if t == nil {
		return Snapshot{}
	}
	finishedAt := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	snapshot := Snapshot{
		Name:       t.name,
		StartedAt:  t.startedAt.Format(time.RFC3339),
		FinishedAt: finishedAt.Format(time.RFC3339),
		DurationMS: durationMilliseconds(finishedAt.Sub(t.startedAt)),
		Details:    make([]Detail, 0, len(t.details)),
	}
	for _, detail := range t.details {
		if detail == nil {
			continue
		}
		current := *detail
		snapshot.Details = append(snapshot.Details, current)
		snapshot.TotalCount += current.Count
		switch current.Action {
		case ActionRead:
			snapshot.ReadCount += current.Count
		case ActionInsert:
			snapshot.InsertCount += current.Count
		case ActionUpdate:
			snapshot.UpdateCount += current.Count
		case ActionDelete:
			snapshot.DeleteCount += current.Count
		case ActionUpsert:
			snapshot.UpsertCount += current.Count
		case ActionSkip:
			snapshot.SkipCount += current.Count
		case ActionError:
			snapshot.ErrorCount += current.Count
		}
	}
	sortDetails(snapshot.Details)
	return snapshot
}

// SnapshotFromContext 返回当前 context 的统计快照；无业务处理量时返回 nil。
func SnapshotFromContext(ctx context.Context) *Snapshot {
	tracker := FromContext(ctx)
	if tracker == nil {
		return nil
	}
	snapshot := tracker.Snapshot()
	if snapshot.Empty() {
		return nil
	}
	return &snapshot
}

// normalizeAction 把动作类型收敛到稳定枚举，避免日志和接口字段发散。
func normalizeAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case ActionRead, ActionInsert, ActionUpdate, ActionDelete, ActionUpsert, ActionSkip, ActionError, ActionCustom:
		return strings.ToLower(strings.TrimSpace(action))
	default:
		return ActionCustom
	}
}

// parseSnapshotTime 解析快照中的 RFC3339 时间，非法时间按缺失处理。
func parseSnapshotTime(value string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

// mergeDetail 合并单条处理量明细，按动作和业务对象聚合。
func mergeDetail(details map[string]*Detail, detail Detail) {
	if details == nil {
		return
	}
	action := normalizeAction(detail.Action)
	name := strings.TrimSpace(detail.Name)
	if name == "" {
		name = DetailNameDefault
	}
	key := action + "\x00" + name
	current := details[key]
	if current == nil {
		current = &Detail{Action: action, Name: name}
		details[key] = current
	}
	if detail.Count > 0 {
		current.Count += detail.Count
	}
	if detail.Times > 0 {
		current.Times += detail.Times
	}
	if detail.ElapsedMS > 0 {
		current.ElapsedMS += detail.ElapsedMS
	}
}

// sortDetails 按动作和业务对象稳定排序处理量明细。
func sortDetails(details []Detail) {
	sort.Slice(details, func(i, j int) bool {
		if details[i].Action == details[j].Action {
			return details[i].Name < details[j].Name
		}
		return details[i].Action < details[j].Action
	})
}

// durationMilliseconds 把耗时统一转换成毫秒，亚毫秒按 1ms 计入。
func durationMilliseconds(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	ms := d.Milliseconds()
	if ms <= 0 {
		return 1
	}
	return ms
}
