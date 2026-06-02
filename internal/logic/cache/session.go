package cache

import (
	keys "admin/common/rediskeys"
	corelogic "admin/internal/logic"
	"admin/internal/model"
	"admin/internal/svc"
	"admin/internal/types"
	"context"
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"
	"github.com/redis/go-redis/v9"
)

// CacheLogic 负责登录态与轻量级业务缓存的读写封装。
type CacheLogic struct {
	*corelogic.BaseLogic // 复用上下文和 Redis 操作能力
}

// NewCacheLogic 为缓存相关逻辑绑定请求上下文，确保 Redis 操作日志能带上 trace_id。
func NewCacheLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CacheLogic {
	return &CacheLogic{
		BaseLogic: corelogic.NewBaseLogicWithContext(ctx, svcCtx),
	}
}

// getAdminInfoKey 统一管理员缓存 key 格式，避免业务层散落拼接字符串。
func (l *CacheLogic) getAdminInfoKey(adminID int) string {
	return keys.AdminInfoRedisKey(adminID)
}

// getAdminLogoutTokenKey 统一管理员登出令牌标记 key，避免中间件和业务层重复拼接。
func (l *CacheLogic) getAdminLogoutTokenKey(adminID int) string {
	return keys.AdminLogoutTokenRedisKey(adminID)
}

// ClearAdminLogoutToken 清理管理员显式登出标记，登录成功后允许新 token 正常回源会话。
func (l *CacheLogic) ClearAdminLogoutToken(adminID int) error {
	if adminID <= 0 {
		return nil
	}
	return l.RdsDelKeys(l.getAdminLogoutTokenKey(adminID))
}

// SetAdminInfo 整体写入管理员缓存信息，并设置统一过期时间。
func (l *CacheLogic) SetAdminInfo(adminID int, info *types.AdminInfo) error {
	ctx := l.Ctx
	key := l.getAdminInfoKey(adminID)
	expiresIn := l.Svc.CurrentConfig().JwtExpiresIn
	if expiresIn <= 0 {
		// 支持测试与轻量 ServiceContext 场景：若未显式注入 JwtExpiresIn，则按默认 24 小时处理，避免缓存立即过期。
		expiresIn = 86400
	}
	pipe := l.Svc.Rds.Pipeline()
	pipe.HSet(ctx, key, info.ToMap())
	pipe.Expire(ctx, key, corelogic.JitterTTL(time.Duration(expiresIn)*time.Second))
	_, err := pipe.Exec(ctx)
	return errors.Tag(err)
}

// SetAdminInfoByField 按字段增量更新管理员缓存，只允许更新已存在的字段。
func (l *CacheLogic) SetAdminInfoByField(adminID int, field string, value any) error {
	ctx := l.Ctx
	key := l.getAdminInfoKey(adminID)
	// 判断缓存和字段都存在时才更新，避免误把不完整数据补写到缓存里。
	result, err := setAdminInfoByFieldScript.Run(ctx, l.Svc.Rds, []string{key}, field, value).Result()
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
	ctx := l.Ctx
	key := l.getAdminInfoKey(adminID)
	var adminInfo types.AdminInfo
	result, err := l.Svc.Rds.HGetAll(ctx, key).Result()
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
	ctx := l.Ctx
	key := l.getAdminInfoKey(adminID)
	return l.Svc.Rds.HGet(ctx, key, "token").Result()
}

// DeleteAdminInfo 删除指定管理员的缓存登录态。
func (l *CacheLogic) DeleteAdminInfo(adminID int) error {
	key := l.getAdminInfoKey(adminID)
	return errors.Tag(l.Svc.Rds.Del(l.Ctx, key).Err())
}

// MarkAdminLogoutToken 记录管理员最近一次显式登出的 token，避免缓存 miss 时误把已登出的 token 回源恢复。
func (l *CacheLogic) MarkAdminLogoutToken(adminID int, token string, ttl time.Duration) error {
	if adminID <= 0 || strings.TrimSpace(token) == "" {
		return nil
	}
	if ttl <= 0 {
		ttl = corelogic.JitterTTL(7 * 24 * time.Hour)
	}
	return errors.Tag(l.Svc.Rds.Set(l.Ctx, l.getAdminLogoutTokenKey(adminID), token, ttl).Err())
}

// IsAdminLogoutToken 判断当前 token 是否已被显式登出标记。
func (l *CacheLogic) IsAdminLogoutToken(adminID int, token string) (bool, error) {
	if adminID <= 0 || strings.TrimSpace(token) == "" {
		return false, nil
	}
	value, err := l.Svc.Rds.Get(l.Ctx, l.getAdminLogoutTokenKey(adminID)).Result()
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
	var admin model.Admin
	if err := l.Svc.WriteDB(svc.DatabaseMain).Where("id = ?", adminID).First(&admin).Error; err != nil {
		return nil, errors.Tag(err)
	}
	info := BuildAdminProfileCache(&admin).ToAdminInfo(token)
	if err := l.SetAdminInfo(adminID, info); err != nil {
		return nil, errors.Tag(err)
	}
	return info, nil
}

// RebuildAdminInfoByKey 根据管理员缓存键重建登录态缓存；仅当原缓存仍携带 token 时允许重建。
func (l *CacheLogic) RebuildAdminInfoByKey(key string) error {
	key = strings.TrimSpace(key)
	adminID, ok := keys.AdminInfoIDFromRedisKey(key)
	if !ok {
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
	ctx := l.Ctx
	key := l.getAdminInfoKey(adminID)
	ttl, err := l.Svc.Rds.TTL(ctx, key).Result()
	if err != nil {
		return errors.Tag(err)
	}
	if ttl < 1*time.Hour {
		return errors.Tag(l.Svc.Rds.Expire(ctx, key, corelogic.JitterTTL(4*time.Hour)).Err())
	}
	return nil
}

// BuildAdminProfileCache 把管理员模型转换成公开资料缓存结构。
func BuildAdminProfileCache(admin *model.Admin) *types.AdminProfile {
	if admin == nil {
		return &types.AdminProfile{}
	}
	return &types.AdminProfile{
		ID:                admin.ID,
		UserName:          admin.Name,
		RealName:          admin.RealName,
		NeedResetPassword: admin.NeedResetPassword,
		Email:             admin.Email,
		Phone:             admin.Phone,
		MfaStatus:         admin.MfaStatus,
		Status:            admin.Status,
		Avatar:            admin.Avatar,
		Description:       admin.Description,
		LastLoginTime:     corelogic.FormatDateTime(admin.LastLoginTime),
		LastLoginIP:       admin.LastLoginIP,
		LastLoginIPAddr:   admin.LastLoginIPAddr,
		CreatedAt:         corelogic.FormatDateTime(admin.CreatedAt),
		UpdatedAt:         corelogic.FormatDateTime(admin.UpdatedAt),
	}
}
