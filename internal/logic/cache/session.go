package cache

import (
	keys "admin/common/rediskeys"
	corelogic "admin/internal/logic"
	"admin/internal/model"
	"admin/internal/svc"
	"admin/internal/types"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"
	"github.com/redis/go-redis/v9"
)

// CacheLogic 负责登录态与轻量级业务缓存的读写封装。
type CacheLogic struct {
	*corelogic.BaseLogic // 复用上下文和 Redis 操作能力
}

const (
	// adminRefreshTokenGraceSeconds 是刷新接口对上一枚 token 的幂等宽限窗口，普通业务接口不使用该窗口。
	adminRefreshTokenGraceSeconds = int64(30)
	// adminRefreshPreviousTokenDigestField 保存上一枚刷新 token 的 SHA-256 摘要，不在 Redis 中留存原 token。
	adminRefreshPreviousTokenDigestField = "refreshPreviousTokenDigest"
	// adminRefreshGraceUntilField 保存上一枚刷新 token 的宽限截止时间戳。
	adminRefreshGraceUntilField = "refreshGraceUntil"
)

// adminInfoMutableFields 是个人中心允许原子同步到当前登录态的公开资料字段。
var adminInfoMutableFields = map[string]struct{}{
	"avatar":            {},
	"description":       {},
	"email":             {},
	"mfaStatus":         {},
	"needResetPassword": {},
	"phone":             {},
	"realName":          {},
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

// SetAdminInfo 整体写入管理员缓存信息，并设置统一过期时间。
func (l *CacheLogic) SetAdminInfo(adminID int, info *types.AdminInfo) error {
	if adminID <= 0 || info == nil {
		return errors.Errorf("管理员会话写入参数不完整")
	}
	ctx := l.Ctx
	key := l.getAdminInfoKey(adminID)
	expiresIn := l.Svc.CurrentConfig().JwtExpiresIn
	if expiresIn <= 0 {
		// 支持测试与轻量 ServiceContext 场景：若未显式注入 JwtExpiresIn，则按默认 24 小时处理，避免缓存立即过期。
		expiresIn = 86400
	}
	// 登录资料、token、宽限字段清理和 TTL 必须在同 key MULTI/EXEC 中提交，避免并发登出穿插 HSET/EXPIRE。
	_, err := l.Svc.Rds.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.HSet(ctx, key, info.ToMap())
		pipe.HDel(ctx, key, adminRefreshPreviousTokenDigestField, adminRefreshGraceUntilField)
		pipe.Expire(ctx, key, time.Duration(expiresIn)*time.Second)
		return nil
	})
	return errors.Tag(err)
}

// SetAdminInfoFields 原子更新当前登录态中已经存在的受控资料字段，不创建已撤销或过期的会话。
func (l *CacheLogic) SetAdminInfoFields(adminID int, fields map[string]any) error {
	if adminID <= 0 || len(fields) == 0 {
		return errors.Errorf("管理员会话字段更新参数不完整")
	}
	fieldNames := make([]string, 0, len(fields))
	for rawField := range fields {
		field := strings.TrimSpace(rawField)
		if field != rawField {
			return errors.Errorf("管理员会话字段格式不合法: %s", rawField)
		}
		if _, ok := adminInfoMutableFields[field]; !ok {
			return errors.Errorf("管理员会话字段不允许更新: %s", field)
		}
		fieldNames = append(fieldNames, field)
	}
	sort.Strings(fieldNames)
	args := make([]any, 0, len(fieldNames)*2)
	for _, field := range fieldNames {
		args = append(args, field, fields[field])
	}
	result, err := setAdminInfoFieldsScript.Run(l.Ctx, l.Svc.Rds, []string{l.getAdminInfoKey(adminID)}, args...).Int64()
	if err != nil {
		return errors.Tag(err)
	}
	if result == 0 {
		return errors.Errorf("管理员会话缓存或目标字段不存在")
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

// RotateAdminToken 原子轮换当前 token；同一旧 token 在短暂宽限期内重试时幂等返回已轮换 token。
// 返回空字符串表示会话不存在、已登出、已重新登录或请求 token 已超出宽限期。
func (l *CacheLogic) RotateAdminToken(adminID int, expectedToken string, newToken string) (string, error) {
	expectedToken = strings.TrimSpace(expectedToken)
	newToken = strings.TrimSpace(newToken)
	if adminID <= 0 || expectedToken == "" || newToken == "" {
		return "", errors.Errorf("管理员会话 token 轮换参数不完整")
	}
	expiresIn := l.Svc.CurrentConfig().JwtExpiresIn
	if expiresIn <= 0 {
		expiresIn = 86400
	}
	result, err := rotateAdminTokenScript.Run(
		l.Ctx,
		l.Svc.Rds,
		[]string{l.getAdminInfoKey(adminID)},
		expectedToken,
		newToken,
		adminTokenDigest(expectedToken),
		adminRefreshTokenGraceSeconds,
		expiresIn,
	).Text()
	if err != nil {
		return "", errors.Tag(err)
	}
	return strings.TrimSpace(result), nil
}

// CanUseAdminSessionToken 判断 token 是否为当前 token，或仍处于会话刷新宽限期。
// 该能力只允许鉴权中间件用于 auth.refresh 和 auth.logout，普通业务路由仍只接受当前 token。
func (l *CacheLogic) CanUseAdminSessionToken(adminID int, token string) (bool, error) {
	token = strings.TrimSpace(token)
	if adminID <= 0 || token == "" {
		return false, nil
	}
	result, err := canUseAdminSessionTokenScript.Run(
		l.Ctx,
		l.Svc.Rds,
		[]string{l.getAdminInfoKey(adminID)},
		token,
		adminTokenDigest(token),
	).Int64()
	if err != nil {
		return false, errors.Tag(err)
	}
	return result == 1, nil
}

// adminTokenDigest 返回 token 的不可逆摘要，供短暂刷新宽限匹配使用。
func adminTokenDigest(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

// DeleteAdminInfo 删除指定管理员的缓存登录态。
func (l *CacheLogic) DeleteAdminInfo(adminID int) error {
	key := l.getAdminInfoKey(adminID)
	return errors.Tag(l.Svc.Rds.Del(l.Ctx, key).Err())
}

// DeleteAdminSessionForLogout 删除当前请求持有的会话。
// 刷新并发窗口内接受上一枚 token；重新登录会清空宽限字段，因此旧登录请求不能删除新会话。
func (l *CacheLogic) DeleteAdminSessionForLogout(adminID int, token string) (bool, error) {
	token = strings.TrimSpace(token)
	if adminID <= 0 || token == "" {
		return false, nil
	}
	deleted, err := deleteAdminSessionForLogoutScript.Run(
		l.Ctx,
		l.Svc.Rds,
		[]string{l.getAdminInfoKey(adminID)},
		token,
		adminTokenDigest(token),
	).Int64()
	if err != nil {
		return false, errors.Tag(err)
	}
	return deleted == 1, nil
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
