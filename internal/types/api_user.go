package types

import (
	"strings"
	"unicode"

	"github.com/Is999/go-utils/errors"
)

const (
	// apiUserPasswordMinLength 表示前台用户后台重置密码的最小长度。
	apiUserPasswordMinLength = 8
	// apiUserPasswordMaxLength 表示前台用户后台重置密码的最大长度。
	apiUserPasswordMaxLength = 64
)

// APIUserListReq 表示前台用户列表查询请求。
type APIUserListReq struct {
	ID       int64  `json:"id,optional" form:"id,optional"`             // 用户 ID 精确筛选
	Username string `json:"username,optional" form:"username,optional"` // 用户名筛选
	Email    string `json:"email,optional" form:"email,optional"`       // 邮箱筛选
	Phone    string `json:"phone,optional" form:"phone,optional"`       // 手机号筛选
	Status   *int   `json:"status,optional" form:"status,optional"`     // 状态筛选：1 正常，0 禁用
	GetOrderReq
	GetPageReq
}

// APIUserIDReq 表示前台用户详情路径请求。
type APIUserIDReq struct {
	ID int64 `path:"id" json:"id,optional" form:"id,optional"` // 用户 ID
}

// Validate 校验前台用户详情路径请求。
func (r *APIUserIDReq) Validate() error {
	if r.ID <= 0 {
		return errors.Errorf("用户 ID 不能为空")
	}
	return nil
}

// Validate 校验前台用户列表查询请求。
func (r *APIUserListReq) Validate() error {
	r.Username = strings.TrimSpace(r.Username)
	r.Email = strings.TrimSpace(r.Email)
	r.Phone = strings.TrimSpace(r.Phone)
	if r.Status != nil && (*r.Status != 0 && *r.Status != 1) {
		return errors.Errorf("用户状态不合法")
	}
	if err := r.GetOrderReq.Validate(); err != nil {
		return errors.Tag(err)
	}
	return r.GetPageReq.Validate()
}

// CreateAPIUserReq 表示后台新增前台用户请求。
type CreateAPIUserReq struct {
	Username     string `json:"username"`              // 用户名，3-32 位
	Password     string `json:"password"`              // 登录密码
	Nickname     string `json:"nickname,optional"`     // 昵称，为空时使用用户名
	Email        string `json:"email,optional"`        // 邮箱
	Phone        string `json:"phone,optional"`        // 手机号
	Avatar       string `json:"avatar,optional"`       // 头像地址
	Status       *int   `json:"status,optional"`       // 状态：1 正常，0 禁用
	TwoStepKey   string `json:"twoStepKey,optional"`   // MFA 二次票据 key
	TwoStepValue string `json:"twoStepValue,optional"` // MFA 二次票据 value
}

// Validate 校验后台新增前台用户请求。
func (r *CreateAPIUserReq) Validate() error {
	r.Username = strings.TrimSpace(r.Username)
	r.Password = strings.TrimSpace(r.Password)
	r.Nickname = strings.TrimSpace(r.Nickname)
	r.Email = strings.TrimSpace(r.Email)
	r.Phone = strings.TrimSpace(r.Phone)
	r.Avatar = strings.TrimSpace(r.Avatar)
	if err := validateAPIUsername(r.Username); err != nil {
		return errors.Tag(err)
	}
	if err := validateAPIUserPassword(r.Password, true); err != nil {
		return errors.Tag(err)
	}
	if r.Status != nil && (*r.Status != 0 && *r.Status != 1) {
		return errors.Errorf("用户状态不合法")
	}
	return nil
}

// UpdateAPIUserReq 表示后台编辑前台用户资料请求。
type UpdateAPIUserReq struct {
	ID           int64   `path:"id" json:"id,optional" form:"id,optional"` // 用户 ID
	Nickname     *string `json:"nickname,optional"`                        // 昵称
	Email        *string `json:"email,optional"`                           // 邮箱
	Phone        *string `json:"phone,optional"`                           // 手机号
	Avatar       *string `json:"avatar,optional"`                          // 头像地址
	TwoStepKey   string  `json:"twoStepKey,optional"`                      // MFA 二次票据 key
	TwoStepValue string  `json:"twoStepValue,optional"`                    // MFA 二次票据 value
}

// Validate 校验后台编辑前台用户资料请求。
func (r *UpdateAPIUserReq) Validate() error {
	if r.ID <= 0 {
		return errors.Errorf("用户 ID 不能为空")
	}
	if r.Nickname != nil {
		value := strings.TrimSpace(*r.Nickname)
		r.Nickname = &value
	}
	if r.Email != nil {
		value := strings.TrimSpace(*r.Email)
		r.Email = &value
	}
	if r.Phone != nil {
		value := strings.TrimSpace(*r.Phone)
		r.Phone = &value
	}
	if r.Avatar != nil {
		value := strings.TrimSpace(*r.Avatar)
		r.Avatar = &value
	}
	return nil
}

// APIUserStatusReq 表示后台修改前台用户状态请求。
type APIUserStatusReq struct {
	ID           int64  `path:"id" json:"id,optional" form:"id,optional"`           // 用户 ID
	Status       *int   `json:"status,optional" form:"status,optional"`             // 状态：1 正常，0 禁用
	TwoStepKey   string `json:"twoStepKey,optional" form:"twoStepKey,optional"`     // MFA 二次票据 key
	TwoStepValue string `json:"twoStepValue,optional" form:"twoStepValue,optional"` // MFA 二次票据 value
}

// Validate 校验后台修改前台用户状态请求。
func (r *APIUserStatusReq) Validate() error {
	if r.ID <= 0 {
		return errors.Errorf("用户 ID 不能为空")
	}
	if r.Status == nil || (*r.Status != 0 && *r.Status != 1) {
		return errors.Errorf("用户状态不合法")
	}
	return nil
}

// ResetAPIUserPasswordReq 表示后台重置前台用户密码请求。
type ResetAPIUserPasswordReq struct {
	ID           int64  `path:"id" json:"id,optional" form:"id,optional"` // 用户 ID
	Password     string `json:"password"`                                 // 新密码
	TwoStepKey   string `json:"twoStepKey,optional"`                      // MFA 二次票据 key
	TwoStepValue string `json:"twoStepValue,optional"`                    // MFA 二次票据 value
}

// Validate 校验后台重置前台用户密码请求。
func (r *ResetAPIUserPasswordReq) Validate() error {
	if r.ID <= 0 {
		return errors.Errorf("用户 ID 不能为空")
	}
	return validateAPIUserPassword(r.Password, true)
}

// SyncAPIUserRuntimeReq 表示手动同步前台用户 API 运行态请求。
type SyncAPIUserRuntimeReq struct {
	ID           int64  `path:"id" json:"id,optional" form:"id,optional"` // 用户 ID
	Profile      bool   `json:"profile,optional"`                         // 是否同步资料缓存
	Sessions     bool   `json:"sessions,optional"`                        // 是否失效登录态
	TwoStepKey   string `json:"twoStepKey,optional"`                      // MFA 二次票据 key
	TwoStepValue string `json:"twoStepValue,optional"`                    // MFA 二次票据 value
}

// Validate 校验手动同步前台用户 API 运行态请求。
func (r *SyncAPIUserRuntimeReq) Validate() error {
	if r.ID <= 0 {
		return errors.Errorf("用户 ID 不能为空")
	}
	if !r.Profile && !r.Sessions {
		r.Profile = true
	}
	return nil
}

// APIUserItem 表示前台用户列表和详情项。
type APIUserItem struct {
	ID          int64  `json:"id"`          // 用户 ID
	Username    string `json:"username"`    // 用户名
	Nickname    string `json:"nickname"`    // 昵称
	Email       string `json:"email"`       // 邮箱
	Phone       string `json:"phone"`       // 手机号
	Avatar      string `json:"avatar"`      // 头像
	Status      int    `json:"status"`      // 状态：1 正常，0 禁用
	LastLoginAt string `json:"lastLoginAt"` // 最后登录时间
	LastLoginIP string `json:"lastLoginIP"` // 最后登录 IP
	CreatedAt   string `json:"createdAt"`   // 创建时间
	UpdatedAt   string `json:"updatedAt"`   // 更新时间
}

// APIUserRuntimeSyncResp 表示 admin 调用 API 运行态同步后的回执。
type APIUserRuntimeSyncResp struct {
	Enabled                 bool   `json:"enabled"`                 // 是否已配置 API 内网同步
	Success                 bool   `json:"success"`                 // 本次同步是否成功
	UserID                  int64  `json:"userId"`                  // 用户 ID
	ProfileCacheInvalidated bool   `json:"profileCacheInvalidated"` // 是否已处理资料缓存
	SessionsInvalidated     bool   `json:"sessionsInvalidated"`     // 是否已处理登录态
	Message                 string `json:"message"`                 // 同步结果说明
}

// APIUserMutationResp 表示前台用户写操作响应。
type APIUserMutationResp struct {
	Item *APIUserItem           `json:"item,omitempty"` // 最新用户资料
	Sync APIUserRuntimeSyncResp `json:"sync"`           // API 运行态同步结果
}

// APIRuntimeConfigReloadReq 表示后台触发 API 配置热加载请求。
type APIRuntimeConfigReloadReq struct {
	TwoStepKey   string `json:"twoStepKey,optional"`   // MFA 二次票据 key
	TwoStepValue string `json:"twoStepValue,optional"` // MFA 二次票据 value
}

// Validate 校验后台触发 API 配置热加载请求。
func (r *APIRuntimeConfigReloadReq) Validate() error {
	return nil
}

// APIRuntimeConfigReloadResp 表示 admin 调用 API 热加载接口后的回执。
type APIRuntimeConfigReloadResp struct {
	Connected bool                        `json:"connected"` // 是否已配置并成功访问 API 内网接口
	Status    *TaskConfigReloadStatusResp `json:"status"`    // API 返回的热加载状态快照
	Message   string                      `json:"message"`   // 调用结果说明
}

// validateAPIUsername 校验前台用户名基础长度。
func validateAPIUsername(username string) error {
	if len(username) < 3 || len(username) > 32 {
		return errors.Errorf("用户名必须为3-32位")
	}
	return nil
}

// validateAPIUserPassword 校验前台用户后台密码。
func validateAPIUserPassword(password string, required bool) error {
	password = strings.TrimSpace(password)
	if password == "" {
		if required {
			return errors.Errorf("密码不能为空")
		}
		return nil
	}
	if len(password) < apiUserPasswordMinLength || len(password) > apiUserPasswordMaxLength {
		return errors.Errorf("密码必须为8-64位")
	}
	for _, item := range password {
		if unicode.Is(unicode.Han, item) {
			return errors.Errorf("密码不能包含中文")
		}
	}
	return nil
}
