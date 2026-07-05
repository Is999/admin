package collectorx

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"admin/common/embedasset"
	keys "admin/common/rediskeys"

	"github.com/Is999/go-utils/errors"
	"github.com/redis/go-redis/v9"
)

const (
	idempotencyProcessing = "processing" // 事件已被当前消费批次占用，等待 Processor 结果
	idempotencyDone       = "done"       // 事件已成功处理
	idempotencyFailed     = "failed"     // 事件已进入失败账本或等待失败账本重试
)

// releaseIdempotencyScriptText 保存安全释放 processing 幂等 key 的 Lua 脚本。
//
//go:embed assets/release_idempotency.lua
var releaseIdempotencyScriptText string

// releaseIdempotencyScriptSource 仅在 token 匹配时删除 processing key，避免误删其它实例占用。
var releaseIdempotencyScriptSource = embedasset.StripLeadingLineComments(releaseIdempotencyScriptText, "--")

// idempotencyStore 使用 Redis 保存 Collector 单任务 EventID 幂等状态，支持跨实例和重启后继续去重。
type idempotencyStore struct {
	redis             redis.UniversalClient // Redis 客户端，作为 Collector 幂等去重状态源
	ttl               time.Duration         // 成功和失败终态保留时间
	processingTTL     time.Duration         // 处理中占用租约时间，进程崩溃后到期允许 Kafka 重投
	pipelineBatchSize int                   // 单次 Redis Pipeline 最大命令数
}

// idempotencyClaim 表示当前批次成功占用的一条 EventID。
type idempotencyClaim struct {
	EventID string // 被占用的事件 ID
	Key     string // Redis 幂等 key
	Token   string // 当前占用 token，用于失败时安全释放
}

// idempotencyStateEvent 表示需要写入 Redis 终态的单任务事件。
type idempotencyStateEvent struct {
	BizType string // 事件所属业务类型
	EventID string // 事件唯一 ID
}

// newIdempotencyStore 创建 Redis 幂等存储。
func newIdempotencyStore(redisClient redis.UniversalClient, ttl time.Duration, processingTTL time.Duration, pipelineBatchSize int) *idempotencyStore {
	if ttl <= 0 {
		ttl = defaultIdempotencyTTL
	}
	if processingTTL <= 0 {
		processingTTL = defaultIdempotencyProcessingTTL
	}
	if pipelineBatchSize <= 0 {
		pipelineBatchSize = defaultIdempotencyPipelineBatchSize
	}
	return &idempotencyStore{
		redis:             redisClient,
		ttl:               ttl,
		processingTTL:     processingTTL,
		pipelineBatchSize: pipelineBatchSize,
	}
}

// beginBatch 批量原子占用 BizType+EventID，使用 Pipeline 减少高吞吐场景下的 Redis 往返。
func (s *idempotencyStore) beginBatch(ctx context.Context, events []Event, now time.Time) ([]Event, []idempotencyClaim, int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil || s.redis == nil {
		return nil, nil, 0, errors.Errorf("collector Redis 幂等存储未初始化")
	}
	if now.IsZero() {
		now = time.Now()
	}
	if len(events) == 0 {
		return events, nil, 0, nil
	}
	out := make([]Event, 0, len(events))
	outClaims := make([]idempotencyClaim, 0, len(events))
	duplicate := 0
	for start := 0; start < len(events); start += s.pipelineLimit() {
		end := start + s.pipelineLimit()
		if end > len(events) {
			end = len(events)
		}
		chunkEvents, chunkClaims, chunkDuplicate, err := s.beginBatchChunk(ctx, events[start:end], now)
		if err != nil {
			return nil, nil, 0, errors.Tag(err)
		}
		out = append(out, chunkEvents...)
		outClaims = append(outClaims, chunkClaims...)
		duplicate += chunkDuplicate
	}
	return out, outClaims, duplicate, nil
}

// beginBatchChunk 执行单个 Redis Pipeline 分块，兼容单机和 Cluster 客户端。
func (s *idempotencyStore) beginBatchChunk(ctx context.Context, events []Event, now time.Time) ([]Event, []idempotencyClaim, int, error) {
	pipe := s.redis.Pipeline()
	commands := make([]*redis.BoolCmd, 0, len(events))
	candidates := make([]Event, 0, len(events))
	claims := make([]idempotencyClaim, 0, len(events))
	for _, event := range events {
		claim, err := s.newClaim(event, now)
		if err != nil {
			return nil, nil, 0, errors.Tag(err)
		}
		candidates = append(candidates, event)
		claims = append(claims, claim)
		commands = append(commands, pipe.SetNX(ctx, claim.Key, claim.Token, s.processingTTL))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, nil, 0, errors.Wrap(err, "collector Redis 幂等批量占用失败")
	}

	out := make([]Event, 0, len(candidates))
	outClaims := make([]idempotencyClaim, 0, len(claims))
	duplicate := 0
	for i, cmd := range commands {
		ok, err := cmd.Result()
		if err != nil {
			return nil, nil, 0, errors.Wrap(err, "collector Redis 幂等占用结果读取失败")
		}
		if !ok {
			duplicate++
			continue
		}
		out = append(out, candidates[i])
		outClaims = append(outClaims, claims[i])
	}
	return out, outClaims, duplicate, nil
}

// newClaim 构造当前事件的 Redis 幂等占用信息。
func (s *idempotencyStore) newClaim(event Event, now time.Time) (idempotencyClaim, error) {
	bizType := strings.TrimSpace(event.BizType)
	eventID := strings.TrimSpace(event.EventID)
	if bizType == "" || eventID == "" {
		return idempotencyClaim{}, errors.Errorf("collector eventId 为空，无法执行幂等去重")
	}
	key := keys.CollectorIdempotencyRedisKey(bizType, eventID)
	if key == "" {
		return idempotencyClaim{}, errors.Errorf("collector Redis 幂等 key 为空 event_id=%s", eventID)
	}
	claim := idempotencyClaim{
		EventID: eventID,
		Key:     key,
		Token:   idempotencyProcessing + ":" + idempotencyToken(eventID, now),
	}
	return claim, nil
}

// done 标记事件已成功处理，后续 Kafka 重投会被跳过。
func (s *idempotencyStore) done(ctx context.Context, events []idempotencyStateEvent, now time.Time) error {
	return s.setState(ctx, events, idempotencyDone, now)
}

// failed 标记事件已由失败账本接管，后续正常 Kafka 重投会被跳过。
func (s *idempotencyStore) failed(ctx context.Context, events []idempotencyStateEvent, now time.Time) error {
	return s.setState(ctx, events, idempotencyFailed, now)
}

// release 仅释放当前批次持有的 processing token，避免误删其它实例的新占用。
func (s *idempotencyStore) release(ctx context.Context, claims []idempotencyClaim) error {
	if len(claims) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil || s.redis == nil {
		return errors.Errorf("collector Redis 幂等存储未初始化")
	}
	for start := 0; start < len(claims); start += s.pipelineLimit() {
		end := start + s.pipelineLimit()
		if end > len(claims) {
			end = len(claims)
		}
		pipe := s.redis.Pipeline()
		valid := 0
		for _, claim := range claims[start:end] {
			if strings.TrimSpace(claim.Key) == "" || strings.TrimSpace(claim.Token) == "" {
				continue
			}
			pipe.Eval(ctx, releaseIdempotencyScriptSource, []string{claim.Key}, claim.Token)
			valid++
		}
		if valid == 0 {
			continue
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return errors.Wrap(err, "collector Redis 幂等释放失败")
		}
	}
	return nil
}

// setState 批量写入终态，使用 Pipeline 降低 Redis 往返次数。
func (s *idempotencyStore) setState(ctx context.Context, events []idempotencyStateEvent, state string, now time.Time) error {
	if len(events) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil || s.redis == nil {
		return errors.Errorf("collector Redis 幂等存储未初始化")
	}
	if now.IsZero() {
		now = time.Now()
	}
	for start := 0; start < len(events); start += s.pipelineLimit() {
		end := start + s.pipelineLimit()
		if end > len(events) {
			end = len(events)
		}
		pipe := s.redis.Pipeline()
		valid := 0
		for _, event := range events[start:end] {
			bizType := strings.TrimSpace(event.BizType)
			eventID := strings.TrimSpace(event.EventID)
			if bizType == "" || eventID == "" {
				continue
			}
			key := keys.CollectorIdempotencyRedisKey(bizType, eventID)
			if key == "" {
				return errors.Errorf("collector Redis 幂等 key 为空 event_id=%s", eventID)
			}
			pipe.Set(ctx, key, state, s.ttl)
			valid++
		}
		if valid == 0 {
			continue
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return errors.Wrap(err, "collector Redis 幂等状态写入失败")
		}
	}
	return nil
}

// pipelineLimit 返回当前 Redis Pipeline 分块上限。
func (s *idempotencyStore) pipelineLimit() int {
	if s == nil || s.pipelineBatchSize <= 0 {
		return defaultIdempotencyPipelineBatchSize
	}
	return s.pipelineBatchSize
}

// idempotencyToken 生成当前占用 token，保证释放时只删除自己持有的 processing 状态。
func idempotencyToken(eventID string, now time.Time) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", strings.TrimSpace(eventID), now.UnixNano())))
	return hex.EncodeToString(sum[:16])
}
