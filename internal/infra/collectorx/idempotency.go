package collectorx

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"admin/common/embedasset"
	keys "admin/common/rediskeys"

	"github.com/Is999/go-utils/errors"
	"github.com/redis/go-redis/v9"
)

const (
	idempotencyProcessingPrefix = "processing:"   // 处理中 token 前缀，用于识别其它 worker 的未完成占用
	idempotencyDone             = "done"          // 事件已成功处理
	idempotencyFailed           = "failed"        // 事件已进入失败账本或等待失败账本重试
	idempotencyRollbackTimeout  = 3 * time.Second // 占用失败后独立回滚的最长等待时间
	idempotencyClaimAcquired    = int64(1)        // 当前批次成功占用
	idempotencyClaimTerminal    = int64(0)        // 已完成或已由失败账本接管
	idempotencyClaimBusy        = int64(-1)       // 其它 worker 正在处理，必须保留 Kafka offset 后重试
	idempotencyClaimInvalid     = int64(-2)       // Redis 中存在未知状态，必须停止提交并排查
)

// claimIdempotencyScriptText 保存区分终态、处理中和非法状态的原子占用脚本。
//
//go:embed assets/claim_idempotency.lua
var claimIdempotencyScriptText string

// claimIdempotencyScriptSource 只在 key 不存在时写入当前批次 token。
var claimIdempotencyScriptSource = embedasset.StripLeadingLineComments(claimIdempotencyScriptText, "--")

// releaseIdempotencyScriptText 保存安全释放 processing 幂等 key 的 Lua 脚本。
//
//go:embed assets/release_idempotency.lua
var releaseIdempotencyScriptText string

// releaseIdempotencyScriptSource 仅在 token 匹配时删除 processing key，避免误删其它实例占用。
var releaseIdempotencyScriptSource = embedasset.StripLeadingLineComments(releaseIdempotencyScriptText, "--")

// transitionIdempotencyScriptText 保存 processing token 续租和终态 CAS 脚本。
//
//go:embed assets/transition_idempotency.lua
var transitionIdempotencyScriptText string

// transitionIdempotencyScriptSource 只有 token 仍匹配时才续租或写入终态。
var transitionIdempotencyScriptSource = embedasset.StripLeadingLineComments(transitionIdempotencyScriptText, "--")

// idempotencyStore 使用 Redis 保存 Collector 单任务 EventID 幂等状态，支持跨实例和重启后继续去重。
type idempotencyStore struct {
	redis             redis.UniversalClient // Redis 客户端，作为 Collector 幂等去重状态源
	ttl               time.Duration         // 成功和失败终态保留时间
	processingTTL     time.Duration         // 处理中占用租约时间，进程崩溃后到期允许 Kafka 重投
	pipelineBatchSize int                   // 单次 Redis Pipeline 最大命令数
}

// idempotencyClaim 表示当前批次成功占用的一条 EventID。
type idempotencyClaim struct {
	BizType string // 事件所属业务类型
	EventID string // 被占用的事件 ID
	Key     string // Redis 幂等 key
	Token   string // 当前占用 token，用于续租、终态回写和安全释放
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
func (s *idempotencyStore) beginBatch(ctx context.Context, events []Event) ([]Event, []idempotencyClaim, int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil || s.redis == nil {
		return nil, nil, 0, errors.Errorf("collector Redis 幂等存储未初始化")
	}
	if len(events) == 0 {
		return events, nil, 0, nil
	}
	uniqueEvents, duplicate := deduplicateIdempotencyEvents(events)
	out := make([]Event, 0, len(uniqueEvents))
	outClaims := make([]idempotencyClaim, 0, len(uniqueEvents))
	for start := 0; start < len(uniqueEvents); start += s.pipelineLimit() {
		end := start + s.pipelineLimit()
		if end > len(uniqueEvents) {
			end = len(uniqueEvents)
		}
		chunkEvents, chunkClaims, chunkDuplicate, err := s.beginBatchChunk(ctx, uniqueEvents[start:end])
		if err != nil {
			return nil, nil, 0, s.rollbackBeginFailure(outClaims, err)
		}
		out = append(out, chunkEvents...)
		outClaims = append(outClaims, chunkClaims...)
		duplicate += chunkDuplicate
	}
	return out, outClaims, duplicate, nil
}

// beginBatchChunk 执行单个 Redis Pipeline 分块，兼容单机和 Cluster 客户端。
func (s *idempotencyStore) beginBatchChunk(ctx context.Context, events []Event) ([]Event, []idempotencyClaim, int, error) {
	pipe := s.redis.Pipeline()
	commands := make([]*redis.Cmd, 0, len(events))
	candidates := make([]Event, 0, len(events))
	claims := make([]idempotencyClaim, 0, len(events))
	for _, event := range events {
		claim, err := s.newClaim(event)
		if err != nil {
			return nil, nil, 0, errors.Tag(err)
		}
		candidates = append(candidates, event)
		claims = append(claims, claim)
		commands = append(commands, pipe.Eval(
			ctx,
			claimIdempotencyScriptSource,
			[]string{claim.Key},
			claim.Token,
			strconv.FormatInt(s.processingTTL.Milliseconds(), 10),
			idempotencyDone,
			idempotencyFailed,
			idempotencyProcessingPrefix,
		))
	}
	_, execErr := pipe.Exec(ctx)

	out := make([]Event, 0, len(candidates))
	outClaims := make([]idempotencyClaim, 0, len(claims))
	duplicate := 0
	var resultErr error
	for i, cmd := range commands {
		state, err := cmd.Int64()
		if err != nil {
			if resultErr == nil {
				resultErr = errors.Wrap(err, "collector Redis 幂等占用结果读取失败")
			}
			continue
		}
		switch state {
		case idempotencyClaimAcquired:
			out = append(out, candidates[i])
			outClaims = append(outClaims, claims[i])
		case idempotencyClaimTerminal:
			duplicate++
		case idempotencyClaimBusy:
			if resultErr == nil {
				resultErr = errors.Errorf("collector 事件正在其它 worker 处理 biz_type=%s event_id=%s", claims[i].BizType, claims[i].EventID)
			}
		case idempotencyClaimInvalid:
			if resultErr == nil {
				resultErr = errors.Errorf("collector Redis 幂等状态非法 biz_type=%s event_id=%s", claims[i].BizType, claims[i].EventID)
			}
		default:
			if resultErr == nil {
				resultErr = errors.Errorf("collector Redis 幂等占用返回未知状态 biz_type=%s event_id=%s state=%d", claims[i].BizType, claims[i].EventID, state)
			}
		}
	}
	if execErr != nil {
		resultErr = errors.Wrap(execErr, "collector Redis 幂等批量占用失败")
	}
	if resultErr != nil {
		// 对全部候选 token 执行 CAS 释放，响应丢失时也能回收已执行但结果未知的占用。
		return nil, nil, 0, s.rollbackBeginFailure(claims, resultErr)
	}
	return out, outClaims, duplicate, nil
}

// deduplicateIdempotencyEvents 按规范化后的 BizType+EventID 稳定去重，避免批内后续项误判为其它 worker 占用。
func deduplicateIdempotencyEvents(events []Event) ([]Event, int) {
	unique := make([]Event, 0, len(events))
	seen := make(map[eventKey]struct{}, len(events))
	duplicate := 0
	for _, event := range events {
		key := eventKey{
			bizType: strings.TrimSpace(event.BizType),
			eventID: strings.TrimSpace(event.EventID),
		}
		if _, ok := seen[key]; ok {
			duplicate++
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, event)
	}
	return unique, duplicate
}

// rollbackBeginFailure 用独立短超时回滚已领取 token，原请求取消时仍尽量避免阻塞到租约到期。
func (s *idempotencyStore) rollbackBeginFailure(claims []idempotencyClaim, cause error) error {
	if len(claims) == 0 {
		return errors.Tag(cause)
	}
	ctx, cancel := context.WithTimeout(context.Background(), idempotencyRollbackTimeout)
	defer cancel()
	if err := s.release(ctx, claims); err != nil {
		return errors.Wrap(err, "collector Redis 幂等占用失败后回滚失败: "+cause.Error())
	}
	return errors.Tag(cause)
}

// newClaim 构造当前事件的 Redis 幂等占用信息。
func (s *idempotencyStore) newClaim(event Event) (idempotencyClaim, error) {
	bizType := strings.TrimSpace(event.BizType)
	eventID := strings.TrimSpace(event.EventID)
	if bizType == "" || eventID == "" {
		return idempotencyClaim{}, errors.Errorf("collector eventId 为空，无法执行幂等去重")
	}
	key := keys.CollectorIdempotencyRedisKey(bizType, eventID)
	if key == "" {
		return idempotencyClaim{}, errors.Errorf("collector Redis 幂等 key 为空 event_id=%s", eventID)
	}
	token, err := newIdempotencyToken()
	if err != nil {
		return idempotencyClaim{}, errors.Wrap(err, "生成 collector Redis 幂等 token 失败")
	}
	claim := idempotencyClaim{
		BizType: bizType,
		EventID: eventID,
		Key:     key,
		Token:   idempotencyProcessingPrefix + token,
	}
	return claim, nil
}

// done 标记事件已成功处理，后续 Kafka 重投会被跳过。
func (s *idempotencyStore) done(ctx context.Context, claims []idempotencyClaim) error {
	if len(claims) == 0 {
		return nil
	}
	if s == nil {
		return errors.Errorf("collector Redis 幂等存储未初始化")
	}
	return s.transition(ctx, claims, idempotencyDone, s.ttl)
}

// failed 标记事件已由失败账本接管，后续正常 Kafka 重投会被跳过。
func (s *idempotencyStore) failed(ctx context.Context, claims []idempotencyClaim) error {
	if len(claims) == 0 {
		return nil
	}
	if s == nil {
		return errors.Errorf("collector Redis 幂等存储未初始化")
	}
	return s.transition(ctx, claims, idempotencyFailed, s.ttl)
}

// renew 仅续期当前批次仍持有的 processing token。
func (s *idempotencyStore) renew(ctx context.Context, claims []idempotencyClaim) error {
	if len(claims) == 0 {
		return nil
	}
	if s == nil {
		return errors.Errorf("collector Redis 幂等存储未初始化")
	}
	return s.transition(ctx, claims, "", s.processingTTL)
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

// transition 以 processing token 为条件批量续租或写入终态，旧领取者不能覆盖新状态。
func (s *idempotencyStore) transition(ctx context.Context, claims []idempotencyClaim, state string, ttl time.Duration) error {
	if len(claims) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil || s.redis == nil {
		return errors.Errorf("collector Redis 幂等存储未初始化")
	}
	if ttl <= 0 {
		return errors.Errorf("collector Redis 幂等状态 TTL 无效")
	}
	for start := 0; start < len(claims); start += s.pipelineLimit() {
		end := start + s.pipelineLimit()
		if end > len(claims) {
			end = len(claims)
		}
		pipe := s.redis.Pipeline()
		commands := make([]*redis.Cmd, 0, end-start)
		for _, claim := range claims[start:end] {
			key := strings.TrimSpace(claim.Key)
			token := strings.TrimSpace(claim.Token)
			if key == "" || token == "" {
				return errors.Errorf("collector Redis 幂等 claim 不完整 event_id=%s", claim.EventID)
			}
			nextState := state
			if nextState == "" {
				nextState = token
			}
			commands = append(commands, pipe.Eval(ctx, transitionIdempotencyScriptSource, []string{key}, token, nextState, strconv.FormatInt(ttl.Milliseconds(), 10)))
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return errors.Wrap(err, "collector Redis 幂等 CAS 执行失败")
		}
		for index, command := range commands {
			matched, err := command.Int64()
			if err != nil {
				return errors.Wrap(err, "collector Redis 幂等 CAS 结果读取失败")
			}
			if matched != 1 {
				claim := claims[start+index]
				return errors.Errorf("collector Redis 幂等 claim 已失效 biz_type=%s event_id=%s", claim.BizType, claim.EventID)
			}
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

// newIdempotencyToken 生成不可复用的随机领取 token。
func newIdempotencyToken() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", errors.Tag(err)
	}
	return hex.EncodeToString(raw), nil
}
