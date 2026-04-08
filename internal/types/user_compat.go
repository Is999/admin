//lint:file-ignore SA5008 ignore go-zero optional tag

package types

import (
	"strings"

	"github.com/Is999/go-utils/errors"
)

// UserCompatLoginReq 表示前端 `vue-vben-admin` 使用的兼容登录请求。
type UserCompatLoginReq struct {
	Username   string `json:"username,optional"`   // 登录用户名
	Password   string `json:"password"`            // 登录密码，沿用前端加密/签名后的传参
	SecureCode string `json:"secureCode,optional"` // 安全验证码预留字段
	Captcha    string `json:"captcha,optional"`    // 图形验证码兼容字段
	Key        string `json:"key,optional"`        // 图形验证码 key 兼容字段
}

// Validate 校验兼容登录请求。
func (r *UserCompatLoginReq) Validate() error {
	r.Username = strings.TrimSpace(r.Username)
	if r.Username == "" {
		return errors.Errorf("用户名不能为空")
	}
	if strings.TrimSpace(r.Password) == "" {
		return errors.Errorf("密码不能为空")
	}
	return nil
}

// UserCheckSecureReq 表示校验当前登录账号密码的请求。
type UserCheckSecureReq struct {
	Secure string `json:"secure"` // 当前登录账号密码
}

// Validate 校验密码校验请求。
func (r *UserCheckSecureReq) Validate() error {
	if strings.TrimSpace(r.Secure) == "" {
		return errors.Errorf("密码不能为空")
	}
	return nil
}

// UserCheckMFAReq 表示校验 MFA 动态码的请求。
type UserCheckMFAReq struct {
	Secure       string `json:"secure"`                // MFA 动态验证码
	Scenarios    int    `json:"scenarios"`             // MFA 校验场景
	MfaSecureKey string `json:"mfaSecureKey,optional"` // 当前绑定流程使用的 TOTP MFA 秘钥
}

// Secret 返回归一化后的 MFA 秘钥。
func (r *UserCheckMFAReq) Secret() string {
	return strings.ToUpper(strings.TrimSpace(r.MfaSecureKey))
}

// Validate 校验 MFA 动态码请求。
func (r *UserCheckMFAReq) Validate() error {
	if strings.TrimSpace(r.Secure) == "" {
		return errors.Errorf("MFA动态验证码不能为空")
	}
	if r.Scenarios < 0 {
		return errors.Errorf("MFA场景不合法")
	}
	if secret := r.Secret(); secret != "" && len(secret) != 16 {
		return errors.Errorf("MFA秘钥长度必须是16位")
	}
	return nil
}

// UserUpdatePasswordReq 表示个人中心修改密码请求。
type UserUpdatePasswordReq struct {
	PasswordOld     string `json:"passwordOld"`           // 旧密码
	PasswordNew     string `json:"passwordNew"`           // 新密码
	ConfirmPassword string `json:"confirmPassword"`       // 确认新密码
	TwoStepKey      string `json:"twoStepKey,optional"`   // 二次校验票据 key
	TwoStepValue    string `json:"twoStepValue,optional"` // 二次校验票据 value
}

// Validate 校验修改密码请求。
func (r *UserUpdatePasswordReq) Validate() error {
	if strings.TrimSpace(r.PasswordOld) == "" {
		return errors.Errorf("旧密码不能为空")
	}
	if err := validateAdminPasswordRequired(r.PasswordNew, "新密码"); err != nil {
		return errors.Tag(err)
	}
	if strings.TrimSpace(r.ConfirmPassword) == "" {
		return errors.Errorf("确认密码不能为空")
	}
	if strings.TrimSpace(r.PasswordNew) != strings.TrimSpace(r.ConfirmPassword) {
		return errors.Errorf("两次输入的新密码不一致")
	}
	return nil
}

// UserUpdateMineReq 表示个人中心更新基础资料请求。
type UserUpdateMineReq struct {
	RealName     string `json:"realName,optional"`     // 真实姓名
	Email        string `json:"email,optional"`        // 邮箱
	Phone        string `json:"phone,optional"`        // 电话
	Avatar       string `json:"avatar,optional"`       // 头像地址
	Description  string `json:"description,optional"`  // 备注说明
	TwoStepKey   string `json:"twoStepKey,optional"`   // 二次校验票据 key
	TwoStepValue string `json:"twoStepValue,optional"` // 二次校验票据 value
}

// Validate 校验个人中心更新请求。
func (r *UserUpdateMineReq) Validate() error {
	r.RealName = strings.TrimSpace(r.RealName)
	r.Email = strings.TrimSpace(r.Email)
	r.Phone = strings.TrimSpace(r.Phone)
	r.Avatar = strings.TrimSpace(r.Avatar)
	r.Description = strings.TrimSpace(r.Description)
	return nil
}

// UserUpdateMFAStatusReq 表示个人中心修改 MFA 状态请求。
type UserUpdateMFAStatusReq struct {
	MfaStatus    *int   `json:"mfaStatus,optional"`    // MFA 状态：0 未启用，1 已启用
	MfaSecureKey string `json:"mfaSecureKey,optional"` // 当前绑定流程使用的 TOTP MFA 秘钥
	TwoStepKey   string `json:"twoStepKey,optional"`   // 二次校验票据 key
	TwoStepValue string `json:"twoStepValue,optional"` // 二次校验票据 value
}

// Status 返回归一化后的 MFA 状态。
func (r *UserUpdateMFAStatusReq) Status() int {
	if r.MfaStatus != nil {
		return *r.MfaStatus
	}
	return -1
}

// Secret 返回归一化后的 MFA 秘钥。
func (r *UserUpdateMFAStatusReq) Secret() string {
	return strings.ToUpper(strings.TrimSpace(r.MfaSecureKey))
}

// Validate 校验修改 MFA 状态请求。
func (r *UserUpdateMFAStatusReq) Validate() error {
	status := r.Status()
	if status != 0 && status != 1 {
		return errors.Errorf("MFA状态不合法")
	}
	if status == 1 && len(r.Secret()) != 16 {
		return errors.Errorf("MFA秘钥长度必须是16位")
	}
	return nil
}

// UserUpdateMFASecureKeyReq 表示个人中心修改 MFA 秘钥请求。
type UserUpdateMFASecureKeyReq struct {
	MfaSecureKey string `json:"mfaSecureKey,optional"` // TOTP MFA 秘钥
	TwoStepKey   string `json:"twoStepKey,optional"`   // 二次校验票据 key
	TwoStepValue string `json:"twoStepValue,optional"` // 二次校验票据 value
}

// Secret 返回归一化后的 MFA 秘钥。
func (r *UserUpdateMFASecureKeyReq) Secret() string {
	return strings.ToUpper(strings.TrimSpace(r.MfaSecureKey))
}

// Validate 校验修改 MFA 秘钥请求。
func (r *UserUpdateMFASecureKeyReq) Validate() error {
	if len(r.Secret()) != 16 {
		return errors.Errorf("MFA秘钥长度必须是16位")
	}
	return nil
}

// UserRefreshMFASecretKeyReq 表示个人中心重新生成 MFA 秘钥请求。
type UserRefreshMFASecretKeyReq struct {
	TwoStepKey   string `json:"twoStepKey,optional"`   // 二次校验票据 key
	TwoStepValue string `json:"twoStepValue,optional"` // 二次校验票据 value
}

// Validate 校验重新生成 MFA 秘钥请求。
func (r *UserRefreshMFASecretKeyReq) Validate() error {
	return nil
}

// UserUpdateAvatarReq 表示个人中心更新头像请求。
type UserUpdateAvatarReq struct {
	Avatar string `json:"avatar"` // 头像地址
}

// Validate 校验更新头像请求。
func (r *UserUpdateAvatarReq) Validate() error {
	if strings.TrimSpace(r.Avatar) == "" {
		return errors.Errorf("头像地址不能为空")
	}
	return nil
}

// AdminMFAStatusReq 表示管理员列表中修改 MFA 状态的请求。
type AdminMFAStatusReq struct {
	ID           int    `path:"id" json:"id,optional" form:"id,optional"` // 管理员 ID
	MfaStatus    *int   `json:"mfaStatus,optional"`                       // MFA 状态：0 未启用，1 已启用
	TwoStepKey   string `json:"twoStepKey,optional"`                      // 二次校验票据 key
	TwoStepValue string `json:"twoStepValue,optional"`                    // 二次校验票据 value
}

// Status 返回归一化后的管理员 MFA 状态。
func (r *AdminMFAStatusReq) Status() int {
	if r.MfaStatus != nil {
		return *r.MfaStatus
	}
	return -1
}

// Validate 校验管理员 MFA 状态请求。
func (r *AdminMFAStatusReq) Validate() error {
	if r.ID <= 0 {
		return errors.Errorf("管理员ID不能为空")
	}
	status := r.Status()
	if status != 0 && status != 1 {
		return errors.Errorf("MFA状态不合法")
	}
	return nil
}

// UserCompatTwoStepResp 表示前端两步校验票据。
type UserCompatTwoStepResp struct {
	Key    string `json:"key"`    // 二次校验票据 key
	Time   int64  `json:"time"`   // 二次校验生成时间
	Expire int64  `json:"expire"` // 二次校验有效期，单位秒
	Value  string `json:"value"`  // 二次校验票据 value
}

// UserCompatMFACheckResp 表示 MFA 校验接口响应。
type UserCompatMFACheckResp struct {
	IsOk        bool                   `json:"isOk"`        // 是否校验成功
	Scenarios   int                    `json:"scenarios"`   // 当前校验场景
	ExistMFA    bool                   `json:"existMFA"`    // 是否已经绑定 MFA 设备
	BuildMFAURL string                 `json:"buildMFAURL"` // MFA 绑定地址
	MFACheck    int                    `json:"mfaCheck"`    // MFA 登录校验状态：0 已完成，1 未完成
	Frequency   int                    `json:"frequency"`   // MFA 校验频率，单位秒
	TwoStep     *UserCompatTwoStepResp `json:"twoStep"`     // 二次校验票据
}

// UserCompatRoleInfo 表示前端使用的角色与权限响应。
type UserCompatRoleInfo struct {
	SuperUserRole            int      `json:"superUserRole"`               // 是否是超级管理员：1 是，0 否
	Roles                    []string `json:"roles"`                       // 当前管理员角色名称列表
	Permissions              []string `json:"permissions"`                 // 当前管理员权限码列表
	CheckMFAScenariosDisable []int    `json:"check_mfa_scenarios_disable"` // 禁用 MFA 校验的场景列表
}

// UserCompatInfo 表示前端 `mine/login` 接口使用的用户信息。
type UserCompatInfo struct {
	ID                int    `json:"id"`                // 管理员 ID
	Username          string `json:"username"`          // 登录用户名
	RealName          string `json:"realName"`          // 真实姓名
	NeedResetPassword int    `json:"needResetPassword"` // 是否必须修改登录密码：0 否，1 是
	Email             string `json:"email"`             // 邮箱
	Phone             string `json:"phone"`             // 电话
	Status            int    `json:"status"`            // 账号状态：1 正常，0 禁用
	MfaStatus         int    `json:"mfaStatus"`         // MFA 状态：0 未启用，1 已启用
	GroupID           int    `json:"groupID"`           // 分组 ID，当前固定返回 0
	ExistMFA          bool   `json:"existMFA"`          // 是否已经绑定 MFA 设备
	BuildMFAURL       string `json:"buildMFAURL"`       // MFA 绑定地址
	ForceMFAEnabled   bool   `json:"forceMFAEnabled"`   // 系统是否开启强制启用 MFA
	MFABindRequired   bool   `json:"mfaBindRequired"`   // 当前登录是否必须先绑定并启用 MFA
	Avatar            string `json:"avatar"`            // 头像地址
	Description       string `json:"description"`       // 备注说明
	LastLoginTime     string `json:"lastLoginTime"`     // 最近登录时间
	LastLoginIP       string `json:"lastLoginIP"`       // 最近登录 IP
	LastLoginAddr     string `json:"lastLoginIpaddr"`   // 最近登录 IP 归属地
	CreatedAt         string `json:"createdAt"`         // 创建时间
	UpdatedAt         string `json:"updatedAt"`         // 更新时间
	MFACheck          int    `json:"mfaCheck"`          // MFA 登录校验状态：0 已完成，1 未完成
	Frequency         int    `json:"frequency"`         // MFA 校验频率，单位秒
}

// UserCompatLoginResp 表示前端兼容登录响应。
type UserCompatLoginResp struct {
	Token string          `json:"token"` // 访问令牌
	User  *UserCompatInfo `json:"user"`  // 当前登录管理员信息
}

// UserBuildMFAURLResp 表示生成 MFA 绑定地址响应。
type UserBuildMFAURLResp struct {
	BuildMFAURL string `json:"buildMFAURL"` // MFA 绑定地址
}

// UserBoolResp 表示简单布尔结果响应。
type UserBoolResp struct {
	IsOk bool `json:"isOk"` // 是否成功
}
