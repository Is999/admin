//lint:file-ignore SA5008 ignore go-zero optional tag

package types

import (
	"fmt"

	"github.com/Is999/go-utils/errors"
)

// LoginReq 表示管理员登录请求参数。
type LoginReq struct {
	Username   string `json:"username"`            // 登录用户名
	Password   string `json:"password"`            // 明文密码，进入逻辑层后会参与校验
	Captcha    string `json:"captcha,optional"`    // 图形验证码内容
	Key        string `json:"key,optional"`        // 图形验证码 key
	SecureCode string `json:"secureCode,optional"` // 安全验证码，参与登录签名与加密
	IP         string `json:"ip,optional"`         // 登录 IP，由 handler 使用可信代理规则覆盖客户端输入
}

// Validate 校验登录参数。
func (r *LoginReq) Validate() error {
	if r.Username == "" {
		return errors.Errorf("用户名不能为空")
	}
	if r.Password == "" {
		return errors.Errorf("密码不能为空")
	}
	return nil
}

// LoginCaptchaResp 表示登录图形验证码响应。
type LoginCaptchaResp struct {
	Key           string `json:"key"`           // 验证码 key
	Image         string `json:"image"`         // 验证码图片，使用 data URL 返回
	ExpireSeconds int    `json:"expireSeconds"` // 验证码有效期（秒）
}

// AddAdminReq 表示新增管理员请求参数。
type AddAdminReq struct {
	Username     string `json:"username"`              // 管理员登录名
	RealName     string `json:"realName"`              // 管理员真实姓名
	Password     string `json:"password"`              // 初始登录密码
	Email        string `json:"email"`                 // 联系邮箱
	Phone        string `json:"phone"`                 // 联系电话
	MfaSecureKey string `json:"mfaSecureKey,optional"` // TOTP MFA 密钥，如 Google Authenticator 使用的种子
	Avatar       string `json:"avatar"`                // 头像地址
	Description  string `json:"description,optional"`  // 备注说明
	RoleIDs      []int  `json:"roleIDs,optional"`      // 初始绑定角色 ID 列表
	TwoStepKey   string `json:"twoStepKey,optional"`   // 二次校验票据 key
	TwoStepValue string `json:"twoStepValue,optional"` // 二次校验票据 value
}

// Validate 校验新增管理员参数。
func (r *AddAdminReq) Validate() error {
	if r.Username == "" {
		return errors.Errorf("用户名不能为空")
	}
	if err := validateAdminPasswordRequired(r.Password, "密码"); err != nil {
		return errors.Tag(err)
	}
	return nil
}

// AdminInfo 表示当前管理员的登录态与基础资料缓存结构。
type AdminInfo struct {
	ID                int    `json:"id"`                // 管理员 ID
	UserName          string `json:"username"`          // 登录用户名
	RealName          string `json:"realName"`          // 真实姓名
	NeedResetPassword int    `json:"needResetPassword"` // 是否必须修改登录密码：0 否，1 是
	Email             string `json:"email"`             // 邮箱
	Phone             string `json:"phone"`             // 电话
	MfaStatus         int    `json:"mfaStatus"`         // MFA 状态：0 未启用，1 已启用
	Status            int    `json:"status"`            // 账户状态：1 正常，0 禁用
	Avatar            string `json:"avatar"`            // 头像地址
	Description       string `json:"description"`       // 备注说明
	LastLoginTime     string `json:"lastLoginTime"`     // 最近登录时间
	LastLoginIP       string `json:"lastLoginIP"`       // 最近登录 IP
	LastLoginIPAddr   string `json:"lastLoginIpaddr"`   // 最近登录 IP 归属地
	Token             string `json:"token"`             // 当前登录态 JWT 令牌
}

// AdminProfile 表示管理员公开资料缓存结构。
// 该结构只保留登录后初始化、个人中心展示和会话重建所需字段，不缓存密码和 MFA 秘钥等敏感信息。
type AdminProfile struct {
	ID                int    `json:"id"`                // 管理员 ID
	UserName          string `json:"username"`          // 登录用户名
	RealName          string `json:"realName"`          // 真实姓名
	NeedResetPassword int    `json:"needResetPassword"` // 是否必须修改登录密码：0 否，1 是
	Email             string `json:"email"`             // 邮箱
	Phone             string `json:"phone"`             // 电话
	MfaStatus         int    `json:"mfaStatus"`         // MFA 状态：0 未启用，1 已启用
	Status            int    `json:"status"`            // 账户状态：1 正常，0 禁用
	Avatar            string `json:"avatar"`            // 头像地址
	Description       string `json:"description"`       // 备注说明
	LastLoginTime     string `json:"lastLoginTime"`     // 最近登录时间
	LastLoginIP       string `json:"lastLoginIP"`       // 最近登录 IP
	LastLoginIPAddr   string `json:"lastLoginIpaddr"`   // 最近登录 IP 归属地
	CreatedAt         string `json:"createdAt"`         // 创建时间
	UpdatedAt         string `json:"updatedAt"`         // 更新时间
}

// ToAdminInfo 把管理员公开资料转换成登录态缓存结构。
func (a *AdminProfile) ToAdminInfo(token string) *AdminInfo {
	if a == nil {
		return &AdminInfo{Token: token}
	}
	return &AdminInfo{
		ID:                a.ID,
		UserName:          a.UserName,
		RealName:          a.RealName,
		NeedResetPassword: a.NeedResetPassword,
		Email:             a.Email,
		Phone:             a.Phone,
		MfaStatus:         a.MfaStatus,
		Status:            a.Status,
		Avatar:            a.Avatar,
		Description:       a.Description,
		LastLoginTime:     a.LastLoginTime,
		LastLoginIP:       a.LastLoginIP,
		LastLoginIPAddr:   a.LastLoginIPAddr,
		Token:             token,
	}
}

// ToMap 将管理员信息转换为适合缓存写入的键值结构。
func (a *AdminInfo) ToMap() map[string]any {
	return map[string]any{
		"id":                a.ID,
		"username":          a.UserName,
		"realName":          a.RealName,
		"needResetPassword": a.NeedResetPassword,
		"email":             a.Email,
		"phone":             a.Phone,
		"mfaStatus":         a.MfaStatus,
		"status":            a.Status,
		"avatar":            a.Avatar,
		"description":       a.Description,
		"lastLoginTime":     a.LastLoginTime,
		"lastLoginIP":       a.LastLoginIP,
		"lastLoginIpaddr":   a.LastLoginIPAddr,
		"token":             a.Token,
	}
}

// FromMap 从缓存读取结果中恢复管理员信息结构。
func (a *AdminInfo) FromMap(m map[string]string) error {
	if v, ok := m["id"]; ok {
		if _, err := fmt.Sscanf(v, "%d", &a.ID); err != nil {
			return errors.Wrap(err, "解析 AdminInfo ID 失败")
		}
	}
	if v, ok := m["username"]; ok {
		a.UserName = v
	}
	if v, ok := m["realName"]; ok {
		a.RealName = v
	}
	if v, ok := m["needResetPassword"]; ok {
		if _, err := fmt.Sscanf(v, "%d", &a.NeedResetPassword); err != nil {
			return errors.Wrap(err, "解析 AdminInfo NeedResetPassword 失败")
		}
	}
	if v, ok := m["email"]; ok {
		a.Email = v
	}
	if v, ok := m["phone"]; ok {
		a.Phone = v
	}
	if v, ok := m["mfaStatus"]; ok {
		if _, err := fmt.Sscanf(v, "%d", &a.MfaStatus); err != nil {
			return errors.Wrap(err, "解析 AdminInfo MfaStatus 失败")
		}
	}
	if v, ok := m["status"]; ok {
		if _, err := fmt.Sscanf(v, "%d", &a.Status); err != nil {
			return errors.Wrap(err, "解析 AdminInfo Status 失败")
		}
	}
	if v, ok := m["avatar"]; ok {
		a.Avatar = v
	}
	if v, ok := m["description"]; ok {
		a.Description = v
	}
	if v, ok := m["lastLoginTime"]; ok {
		a.LastLoginTime = v
	}
	if v, ok := m["lastLoginIP"]; ok {
		a.LastLoginIP = v
	}
	if v, ok := m["lastLoginIpaddr"]; ok {
		a.LastLoginIPAddr = v
	}
	if v, ok := m["token"]; ok {
		a.Token = v
	}

	return nil
}

// AdminLoginAfterInfoResp 表示前端登录完成后初始化页面所需的管理员资料。
type AdminLoginAfterInfoResp struct {
	ID                int      `json:"id"`                // 管理员 ID，前端登录态初始化必需字段
	UserName          string   `json:"username"`          // 登录用户名，前端登录态初始化必需字段
	RealName          string   `json:"realName"`          // 真实姓名，前端登录态初始化必需字段
	NeedResetPassword int      `json:"needResetPassword"` // 是否必须修改登录密码：0 否，1 是
	IsSuperAdmin      bool     `json:"isSuperAdmin"`      // 是否为超级管理员；前端据此做超级管理员权限兜底
	Email             string   `json:"email"`             // 邮箱
	Phone             string   `json:"phone"`             // 电话
	MfaStatus         int      `json:"mfaStatus"`         // MFA 状态：0 未启用，1 已启用
	Status            int      `json:"status"`            // 账户状态：1 正常，0 禁用
	Avatar            string   `json:"avatar"`            // 头像地址，前端登录态初始化必需字段
	Description       string   `json:"description"`       // 备注说明，前端登录态初始化必需字段
	LastLoginTime     string   `json:"lastLoginTime"`     // 最近登录时间
	LastLoginIP       string   `json:"lastLoginIP"`       // 最近登录 IP
	LastLoginIPAddr   string   `json:"lastLoginIpaddr"`   // 最近登录 IP 归属地
	RoleIDs           []int    `json:"roleIds"`           // 当前账号启用角色 ID 列表；前端据此识别超级管理员角色
	Roles             []string `json:"roles"`             // 角色列表，供前端权限展示与布局判断
	Token             string   `json:"token"`             // JWT 令牌，前端登录态初始化必需字段
}
