package logic

import (
	"context"
	"strings"
	"time"

	codes "admin_cron/common/codes"
	i18n "admin_cron/common/i18n"
	"admin_cron/internal/model"
	"admin_cron/internal/svc"
	"admin_cron/internal/types"

	"github.com/Is999/go-utils/errors"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	// adminBootstrapOperationReset 表示本次内网接口仅执行既有超级管理员账号重置。
	adminBootstrapOperationReset = "reset"
)

// AdminBootstrapLogic 负责仅内网可调用的管理员自举初始化逻辑。
type AdminBootstrapLogic struct {
	*BaseLogic // 复用上下文、数据库、Redis 与日志能力
}

// NewAdminBootstrapLogic 创建管理员自举逻辑对象。
func NewAdminBootstrapLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminBootstrapLogic {
	return &AdminBootstrapLogic{
		BaseLogic: NewBaseLogicWithContext(ctx, svcCtx),
	}
}

// InitAdminBootstrap 通过内网接口重置既有超级管理员账号到首次登录前状态。
func (l *AdminBootstrapLogic) InitAdminBootstrap(req *types.InitAdminBootstrapReq) *types.BizResult {
	if req == nil {
		return types.ParamError(errors.Errorf("初始化请求不能为空")).ToBizResult()
	}
	if err := req.Validate(); err != nil {
		return types.ParamError(err).ToBizResult()
	}

	admin, err := l.resetExistingSuperAdmin(req)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.NotFound(i18n.MsgKeyUserNotFound, err,
				"AdminBootstrapLogic.InitAdminBootstrap 管理员账号[%s]不存在", req.Username).ToBizResult()
		}
		if errors.Is(err, errAdminBootstrapTargetNotSuperAdmin) {
			return types.NewBizResult(codes.Forbidden).
				SetI18nMessage(i18n.MsgKeyForbidden).
				WithError(err)
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminBootstrapLogic.InitAdminBootstrap 重置管理员账号[%s]失败", req.Username).ToBizResult()
	}

	resp := &types.InitAdminBootstrapResp{
		ID:                admin.ID,
		Username:          admin.Name,
		RoleID:            adminSuperRoleID,
		NeedResetPassword: admin.NeedResetPassword,
		MfaStatus:         admin.MfaStatus,
		Status:            admin.Status,
		Operation:         adminBootstrapOperationReset,
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeySuccess).
		WithData(resp)
}

var (
	// errAdminBootstrapTargetNotSuperAdmin 表示目标账号不是超级管理员，禁止通过内网接口重置。
	errAdminBootstrapTargetNotSuperAdmin = errors.Errorf("仅允许重置已有超级管理员账号")
)

// resetExistingSuperAdmin 在事务内只允许重置既有超级管理员账号，不允许新建账号或调整角色关系。
func (l *AdminBootstrapLogic) resetExistingSuperAdmin(req *types.InitAdminBootstrapReq) (*model.Admin, error) {
	var admin *model.Admin
	if err := l.svc.WriteDB(svc.DatabaseMain).Transaction(func(tx *gorm.DB) error {
		existing, err := model.FindUserByName(tx, req.Username)
		if err != nil {
			return errors.Wrapf(err, "AdminBootstrapLogic.resetExistingSuperAdmin 查询账号[%s]失败", req.Username)
		}

		if existing == nil {
			return gorm.ErrRecordNotFound
		}
		isSuperAdmin, checkErr := l.bootstrapTargetIsSuperAdmin(existing.ID)
		if checkErr != nil {
			return errors.Wrapf(checkErr, "AdminBootstrapLogic.resetExistingSuperAdmin 校验账号[%s]超级管理员角色失败", req.Username)
		}
		if !isSuperAdmin {
			return errAdminBootstrapTargetNotSuperAdmin
		}

		resetAdmin, resetErr := l.resetBootstrapAdminTx(tx, existing, req)
		if resetErr != nil {
			return resetErr
		}
		admin = resetAdmin
		return nil
	}); err != nil {
		return nil, errors.Tag(err)
	}

	if admin == nil {
		return nil, errors.Errorf("AdminBootstrapLogic.resetExistingSuperAdmin 未生成管理员结果")
	}

	invalidateAdminRelationCache(l.BaseLogic, admin.ID)
	securityLogic := NewSecurityLogic(l.Context(), l.svc)
	_ = securityLogic.ClearLoginMFACompleted(admin.ID)
	l.clearAdminMFATwoStepTickets(admin.ID)
	return admin, nil
}

// resetBootstrapAdminTx 在事务内把既有超级管理员重置为首次登录前状态，不修改角色关系。
func (l *AdminBootstrapLogic) resetBootstrapAdminTx(tx *gorm.DB, admin *model.Admin, req *types.InitAdminBootstrapReq) (*model.Admin, error) {
	if admin == nil {
		return nil, errors.Errorf("管理员不存在")
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(admin.PasswordWithSalt(strings.TrimSpace(req.Password))), bcrypt.DefaultCost)
	if err != nil {
		return nil, errors.Wrapf(err, "AdminBootstrapLogic.resetBootstrapAdminTx 生成账号[%s]密码哈希失败", req.Username)
	}

	updates := map[string]any{
		"password":            string(passwordHash),
		"real_name":           chooseBootstrapString(req.RealName, admin.RealName),
		"need_reset_password": 1,
		"email":               chooseBootstrapString(req.Email, admin.Email),
		"phone":               chooseBootstrapString(req.Phone, admin.Phone),
		"mfa_status":          0,
		"mfa_secure_key":      "",
		"status":              1,
		"avatar":              chooseBootstrapString(req.Avatar, admin.Avatar),
		"description":         chooseBootstrapString(req.Description, admin.Description),
		"last_login_time":     time.Time{},
		"last_login_ip":       "",
		"last_login_ipaddr":   "",
		"updated_at":          time.Now(),
	}
	if err := tx.Model(&model.Admin{}).Where("id = ?", admin.ID).Updates(updates).Error; err != nil {
		return nil, errors.Wrapf(err, "AdminBootstrapLogic.resetBootstrapAdminTx 重置账号[%s]首次状态失败", req.Username)
	}

	admin.RealName = chooseBootstrapString(req.RealName, admin.RealName)
	admin.Password = string(passwordHash)
	admin.NeedResetPassword = 1
	admin.Email = chooseBootstrapString(req.Email, admin.Email)
	admin.Phone = chooseBootstrapString(req.Phone, admin.Phone)
	admin.MfaStatus = 0
	admin.MfaSecureKey = ""
	admin.Status = 1
	admin.Avatar = chooseBootstrapString(req.Avatar, admin.Avatar)
	admin.Description = chooseBootstrapString(req.Description, admin.Description)
	admin.LastLoginTime = time.Time{}
	admin.LastLoginIP = ""
	admin.LastLoginIpaddr = ""
	admin.UpdatedAt = time.Now()
	return admin, nil
}

// bootstrapTargetIsSuperAdmin 判断目标管理员当前是否已拥有超级管理员角色。
func (l *AdminBootstrapLogic) bootstrapTargetIsSuperAdmin(adminID int) (bool, error) {
	roleIDs, err := NewSecurityLogic(l.Context(), l.svc).enabledRoleIDs(adminID)
	if err != nil {
		return false, errors.Tag(err)
	}
	for _, roleID := range roleIDs {
		if roleID == adminSuperRoleID {
			return true, nil
		}
	}
	return false, nil
}

// clearAdminMFATwoStepTickets 清理管理员历史 MFA 二次票据，避免旧票据影响首次上线初始化后的状态。
func (l *AdminBootstrapLogic) clearAdminMFATwoStepTickets(adminID int) {
	if err := NewSecurityLogic(l.Context(), l.svc).ClearAdminMFATwoStepTickets(adminID); err != nil {
		logWrappedError(l.Logger, err, "AdminBootstrapLogic.clearAdminMFATwoStepTickets 清理管理员ID[%d]MFA二次票据失败", adminID)
	}
}

// chooseBootstrapString 在内网初始化时优先采用显式传入的新值，否则保留既有字段值。
func chooseBootstrapString(next string, fallback string) string {
	next = strings.TrimSpace(next)
	if next != "" {
		return next
	}
	return strings.TrimSpace(fallback)
}
