package logic

import (
	keys "admin_cron/common/rediskeys"
	"admin_cron/internal/svc"
	"admin_cron/internal/types"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"
	"github.com/redis/go-redis/v9"
)

// CacheLogic 负责登录态与轻量级业务缓存的读写封装。
type CacheLogic struct {
	*BaseLogic // 复用上下文和 Redis 操作能力
}

// NewCacheLogic 为缓存相关逻辑绑定请求上下文，确保 Redis 操作日志能带上 trace_id。
func NewCacheLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CacheLogic {
	return &CacheLogic{
		BaseLogic: NewBaseLogicWithContext(ctx, svcCtx),
	}
}

// getAdminInfoKey 统一管理员缓存 key 格式，避免业务层散落拼接字符串。
func (l *CacheLogic) getAdminInfoKey(adminID int) string {
	return fmt.Sprintf(keys.AdminInfo, adminID)
}

// getAdminLogoutTokenKey 统一管理员登出令牌标记 key，避免中间件和业务层重复拼接。
func (l *CacheLogic) getAdminLogoutTokenKey(adminID int) string {
	return fmt.Sprintf(keys.AdminLogoutToken, adminID)
}

// SetAdminInfo 整体写入管理员缓存信息，并设置统一过期时间。
func (l *CacheLogic) SetAdminInfo(adminID int, info *types.AdminInfo) error {
	ctx := l.Context()
	key := l.getAdminInfoKey(adminID)
	expiresIn := l.svc.CurrentConfig().JwtExpiresIn
	if expiresIn <= 0 {
		// 兼容测试与轻量 ServiceContext 场景：若未显式注入 JwtExpiresIn，则按默认 24 小时处理，避免缓存立即过期。
		expiresIn = 86400
	}
	pipe := l.svc.Rds.Pipeline()
	pipe.HSet(ctx, key, info.ToMap())
	pipe.Expire(ctx, key, jitterTTL(time.Duration(expiresIn)*time.Second))
	_, err := pipe.Exec(ctx)
	return errors.Tag(err)
}

// SetAdminInfoByField 按字段增量更新管理员缓存，只允许更新已存在的字段。
func (l *CacheLogic) SetAdminInfoByField(adminID int, field string, value any) error {
	ctx := l.Context()
	key := l.getAdminInfoKey(adminID)
	// 判断缓存和字段都存在时才更新，避免误把不完整数据补写到缓存里。
	result, err := setAdminInfoByFieldScript.Run(ctx, l.svc.Rds, []string{key}, field, value).Result()
	if err != nil {
		return errors.Tag(err)
	}
	if result.(int64) == 0 {
		return errors.Errorf("缓存键或字段不存在")
	}
	return nil
}

// GetAdminInfo 读取管理员缓存信息，并反序列化成结构体。
func (l *CacheLogic) GetAdminInfo(adminID int) (*types.AdminInfo, error) {
	ctx := l.Context()
	key := l.getAdminInfoKey(adminID)
	var adminInfo types.AdminInfo
	result, err := l.svc.Rds.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, errors.Tag(err)
	}
	if len(result) == 0 {
		return nil, redis.Nil
	}
	err = adminInfo.FromMap(result)
	if err != nil {
		return nil, errors.Tag(err)
	}
	return &adminInfo, nil
}

// GetAdminToken 读取管理员缓存中的访问令牌。
func (l *CacheLogic) GetAdminToken(adminID int) (string, error) {
	ctx := l.Context()
	key := l.getAdminInfoKey(adminID)
	return l.svc.Rds.HGet(ctx, key, "token").Result()
}

// DeleteAdminInfo 删除指定管理员的缓存登录态。
func (l *CacheLogic) DeleteAdminInfo(adminID int) error {
	key := l.getAdminInfoKey(adminID)
	return errors.Tag(l.svc.Rds.Del(l.Context(), key).Err())
}

// MarkAdminLogoutToken 记录管理员最近一次显式登出的 token，避免缓存 miss 时误把旧 token 回源恢复。
func (l *CacheLogic) MarkAdminLogoutToken(adminID int, token string, ttl time.Duration) error {
	if adminID <= 0 || strings.TrimSpace(token) == "" {
		return nil
	}
	if ttl <= 0 {
		ttl = jitterTTL(7 * 24 * time.Hour)
	}
	return errors.Tag(l.svc.Rds.Set(l.Context(), l.getAdminLogoutTokenKey(adminID), token, ttl).Err())
}

// IsAdminLogoutToken 判断当前 token 是否已被显式登出标记。
func (l *CacheLogic) IsAdminLogoutToken(adminID int, token string) (bool, error) {
	if adminID <= 0 || strings.TrimSpace(token) == "" {
		return false, nil
	}
	value, err := l.svc.Rds.Get(l.Context(), l.getAdminLogoutTokenKey(adminID)).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, errors.Tag(err)
	}
	return value == token, nil
}

// RebuildAdminInfo 从主库回源管理员资料并重建登录态缓存。
func (l *CacheLogic) RebuildAdminInfo(adminID int, token string) (*types.AdminInfo, error) {
	if adminID <= 0 {
		return nil, errors.Errorf("管理员ID不能为空")
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.Errorf("管理员登录态Token不能为空")
	}
	profile, err := (&AdminLogic{BaseLogic: l.BaseLogic}).GetAdminProfileByID(adminID)
	if err != nil {
		return nil, errors.Tag(err)
	}
	info := profile.ToAdminInfo(token)
	if err := l.SetAdminInfo(adminID, info); err != nil {
		return nil, errors.Tag(err)
	}
	return info, nil
}

// RebuildAdminInfoByKey 根据管理员缓存键重建登录态缓存；仅当原缓存仍携带 token 时允许重建。
func (l *CacheLogic) RebuildAdminInfoByKey(key string) error {
	key = strings.TrimSpace(key)
	adminIDText := strings.TrimPrefix(key, "admin:info:")
	adminID, err := strconv.Atoi(adminIDText)
	if err != nil || adminID <= 0 {
		return errors.Errorf("管理员登录态缓存key不合法: %s", key)
	}
	token, err := l.GetAdminToken(adminID)
	if err != nil {
		if err == redis.Nil {
			return errors.Errorf("管理员登录态缓存不存在，无法重建: %s", key)
		}
		return errors.Tag(err)
	}
	_, err = l.RebuildAdminInfo(adminID, token)
	return errors.Tag(err)
}

// TouchAdminInfo 为管理员缓存做滑动续期，避免活跃会话频繁回源数据库。
func (l *CacheLogic) TouchAdminInfo(adminID int) error {
	// 判断缓存过期时间是否小于 1 小时，如果是则续期 4 小时，减少频繁写 Redis。
	ctx := l.Context()
	key := l.getAdminInfoKey(adminID)
	ttl, err := l.svc.Rds.TTL(ctx, key).Result()
	if err != nil {
		return errors.Tag(err)
	}
	if ttl < 1*time.Hour {
		return errors.Tag(l.svc.Rds.Expire(ctx, key, jitterTTL(4*time.Hour)).Err())
	}
	return nil
}
