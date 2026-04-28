package rbac

import (
	keys "admin/common/rediskeys"
	corelogic "admin/internal/logic"
	cachelogic "admin/internal/logic/cache"
	"admin/internal/model"
	"admin/internal/svc"
	"fmt"
	"net/http"

	"github.com/Is999/go-utils/errors"
	tablecache "github.com/Is999/table-cache"
)

// AdminRoleRelLogic 处理管理员与角色关系相关逻辑。
type AdminRoleRelLogic struct {
	*corelogic.BaseLogic // 复用上下文、数据库和日志能力
}

// NewAdminRoleRelLogic 创建管理员角色关系逻辑对象。
func NewAdminRoleRelLogic(r *http.Request, svcCtx *svc.ServiceContext) *AdminRoleRelLogic {
	return &AdminRoleRelLogic{
		BaseLogic: corelogic.NewBaseLogic(r, svcCtx),
	}
}

// GetRolesByUserID 根据管理员 ID 获取角色列表。
func (l *AdminRoleRelLogic) GetRolesByUserID(userID int64) ([]string, error) {
	if userID <= 0 {
		return []string{}, nil
	}
	if l.Redis() != nil {
		manager, err := cachelogic.TableCacheManager(l.BaseLogic)
		if err != nil {
			return nil, errors.Wrap(err, "AdminRoleRelLogic.GetRolesByUserID 获取表缓存管理器失败")
		}
		roles := make([]string, 0)
		result, err := manager.LoadThrough(l.Ctx, cachelogic.TableCachePhysicalKey(l.BaseLogic, fmt.Sprintf(keys.AdminRolesDetail, userID)), &roles, nil)
		if err != nil {
			return nil, errors.Wrapf(err, "AdminRoleRelLogic.GetRolesByUserID 加载管理员ID[%d]角色名称缓存失败", userID)
		}
		if result.State == tablecache.LookupStateEmpty {
			return []string{}, nil
		}
		return roles, nil
	}
	roleLogic := &AdminRoleLogic{BaseLogic: l.BaseLogic}
	roleIDs, err := roleLogic.enabledRoleIDsByUserWithCache(int(userID))
	if err != nil {
		return nil, errors.Wrapf(err, "AdminRoleRelLogic.GetRolesByUserID 查询管理员ID[%d]启用角色失败", userID)
	}
	if len(roleIDs) == 0 {
		return []string{}, nil
	}
	var roles []string
	err = l.Svc.ReadDB(svc.DatabaseMain).Model(&model.AdminRole{}).
		Where("id IN ? AND is_delete = 0", roleIDs).
		Order("id ASC").
		Pluck("title", &roles).Error
	if err != nil {
		return nil, errors.Wrapf(err, "AdminRoleRelLogic.GetRolesByUserID 查询管理员ID[%d]角色名称失败", userID)
	}
	return roles, nil
}
