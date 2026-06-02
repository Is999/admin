package security

import (
	corelogic "admin/internal/logic"

	"admin/internal/model"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
)

// BuildProfileInfo 构造前端登录态用户资料。
func (l *SecurityLogic) BuildProfileInfo(admin *model.Admin, token string) (*types.ProfileInfo, error) {
	if admin == nil {
		return nil, errors.Errorf("管理员信息不能为空")
	}
	forceMFAEnabled := l.ForceLoginMFAEnabled()
	existMFA := l.HasUsableAdminMFASecret(admin)
	buildURL := ""
	needBindMFA := l.NeedBindMFAOnLogin(admin)
	if admin.MfaStatus != 1 {
		var err error
		buildURL, err = l.BuildFreshAdminMFAURL(admin)
		if err != nil {
			return nil, errors.Wrapf(err, "SecurityLogic.BuildProfileInfo 生成管理员ID[%d]MFA绑定地址失败", admin.ID)
		}
	}
	mfaCheck := 0
	if l.NeedLoginMFA(admin) && !l.HasPassedLoginMFA(admin) {
		mfaCheck = 1
	}
	return &types.ProfileInfo{
		ID:                admin.ID,
		Username:          admin.Name,
		RealName:          admin.RealName,
		NeedResetPassword: admin.NeedResetPassword,
		Email:             admin.Email,
		Phone:             admin.Phone,
		Status:            admin.Status,
		MfaStatus:         admin.MfaStatus,
		GroupID:           0,
		ExistMFA:          existMFA,
		BuildMFAURL:       buildURL,
		ForceMFAEnabled:   forceMFAEnabled,
		MFABindRequired:   forceMFAEnabled && needBindMFA,
		Avatar:            admin.Avatar,
		Description:       admin.Description,
		LastLoginTime:     corelogic.FormatDateTime(admin.LastLoginTime),
		LastLoginIP:       admin.LastLoginIP,
		LastLoginAddr:     admin.LastLoginIPAddr,
		CreatedAt:         corelogic.FormatDateTime(admin.CreatedAt),
		UpdatedAt:         corelogic.FormatDateTime(admin.UpdatedAt),
		MFACheck:          mfaCheck,
		Frequency:         l.MFAFrequency(),
	}, nil
}
