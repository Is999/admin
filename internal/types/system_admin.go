package types

import (
	"strings"

	"github.com/Is999/go-utils/errors"
)

// IDPathReq 表示只携带路径 ID 的通用请求。
type IDPathReq struct {
	ID           int    `path:"id" json:"id,optional" form:"id,optional"`           // 资源 ID
	TwoStepKey   string `json:"twoStepKey,optional" form:"twoStepKey,optional"`     // 二次校验票据 key
	TwoStepValue string `json:"twoStepValue,optional" form:"twoStepValue,optional"` // 二次校验票据 value
}

// Validate 校验路径 ID 请求。
func (r *IDPathReq) Validate() error {
	if r.ID <= 0 {
		return errors.Errorf("ID不能为空")
	}
	return nil
}

// UUIDPathReq 表示只携带路径 UUID 的通用请求。
type UUIDPathReq struct {
	UUID string `path:"uuid" json:"uuid,optional" form:"uuid,optional"` // 资源 UUID
}

// Validate 校验路径 UUID 请求。
func (r *UUIDPathReq) Validate() error {
	r.UUID = strings.TrimSpace(r.UUID)
	if r.UUID == "" {
		return errors.Errorf("UUID不能为空")
	}
	return nil
}

// AdminListReq 表示管理员列表查询请求。
type AdminListReq struct {
	Username  string `json:"username,optional" form:"username,optional"`   // 登录用户名筛选
	RealName  string `json:"realName,optional" form:"realName,optional"`   // 真实姓名筛选
	Status    *int   `json:"status,optional" form:"status,optional"`       // 账号状态筛选：1 正常，0 禁用
	RoleID    *int   `json:"roleID,optional" form:"roleID,optional"`       // 角色 ID 筛选
	CreatedAt string `json:"createdAt,optional" form:"createdAt,optional"` // 创建时间筛选预留字段
	GetOrderReq
	GetPageReq
}

// Validate 校验管理员列表查询请求。
func (r *AdminListReq) Validate() error {
	r.Username = strings.TrimSpace(r.Username)
	r.RealName = strings.TrimSpace(r.RealName)
	r.CreatedAt = strings.TrimSpace(r.CreatedAt)
	if err := r.GetOrderReq.Validate(); err != nil {
		return errors.Tag(err)
	}
	return r.GetPageReq.Validate()
}

// UpdateAdminReq 表示编辑管理员请求。
type UpdateAdminReq struct {
	ID            int     `path:"id" json:"id,optional" form:"id,optional"`                       // 管理员 ID
	RealName      *string `json:"realName,optional"`                                              // 真实姓名
	Email         *string `json:"email,optional"`                                                 // 邮箱
	Phone         *string `json:"phone,optional"`                                                 // 电话
	MfaSecureKey  *string `json:"mfaSecureKey,optional"`                                          // TOTP MFA 密钥
	MfaStatus     *int    `json:"mfaStatus,optional"`                                             // MFA 状态：0 未启用，1 已启用
	Status        *int    `json:"status,optional"`                                                // 账号状态：1 正常，0 禁用
	Avatar        *string `json:"avatar,optional"`                                                // 头像地址
	Description   *string `json:"description,optional"`                                           // 备注说明
	Password      *string `json:"password,optional"`                                              // 新密码，非空时重置
	RoleIDs       []int   `json:"roleIDs,optional"`                                               // 角色 ID 列表
	IsUpdateRoles bool    `json:"isUpdateRoles,optional" form:"isUpdateRoles,optional,default=0"` // 是否同步更新角色
	TwoStepKey    string  `json:"twoStepKey,optional"`                                            // 二次校验票据 key
	TwoStepValue  string  `json:"twoStepValue,optional"`                                          // 二次校验票据 value
}

// Validate 校验编辑管理员请求。
func (r *UpdateAdminReq) Validate() error {
	if r.ID <= 0 {
		return errors.Errorf("管理员ID不能为空")
	}
	if r.Status != nil && (*r.Status != 0 && *r.Status != 1) {
		return errors.Errorf("账号状态不合法")
	}
	if r.MfaStatus != nil && (*r.MfaStatus != 0 && *r.MfaStatus != 1) {
		return errors.Errorf("MFA状态不合法")
	}
	if r.Password != nil {
		if err := validateAdminPasswordOptional(*r.Password, "密码"); err != nil {
			return errors.Tag(err)
		}
	}
	return nil
}

// AdminStatusReq 表示修改管理员状态请求。
type AdminStatusReq struct {
	ID           int    `path:"id" json:"id,optional" form:"id,optional"`           // 管理员 ID
	Status       *int   `json:"status,optional" form:"status,optional"`             // 账号状态：1 正常，0 禁用
	TwoStepKey   string `json:"twoStepKey,optional" form:"twoStepKey,optional"`     // 二次校验票据 key
	TwoStepValue string `json:"twoStepValue,optional" form:"twoStepValue,optional"` // 二次校验票据 value
}

// StatusValue 返回归一化后的管理员状态值。
func (r *AdminStatusReq) StatusValue() int {
	if r.Status == nil {
		return -1
	}
	return *r.Status
}

// Validate 校验管理员状态请求。
func (r *AdminStatusReq) Validate() error {
	if r.ID <= 0 {
		return errors.Errorf("管理员ID不能为空")
	}
	if status := r.StatusValue(); status != 0 && status != 1 {
		return errors.Errorf("账号状态不合法")
	}
	return nil
}

// ResetAdminPasswordReq 表示重置管理员密码请求。
type ResetAdminPasswordReq struct {
	ID           int    `path:"id" json:"id,optional" form:"id,optional"` // 管理员 ID
	Password     string `json:"password"`                                 // 新密码
	TwoStepKey   string `json:"twoStepKey,optional"`                      // 二次校验票据 key
	TwoStepValue string `json:"twoStepValue,optional"`                    // 二次校验票据 value
}

// Validate 校验重置管理员密码请求。
func (r *ResetAdminPasswordReq) Validate() error {
	if r.ID <= 0 {
		return errors.Errorf("管理员ID不能为空")
	}
	if err := validateAdminPasswordRequired(r.Password, "密码"); err != nil {
		return errors.Tag(err)
	}
	return nil
}

// ResetAdminInitialStateReq 表示把管理员重置为首次登录前状态的请求。
type ResetAdminInitialStateReq struct {
	ID           int    `path:"id" json:"id,optional" form:"id,optional"` // 管理员 ID
	Password     string `json:"password"`                                 // 临时密码
	TwoStepKey   string `json:"twoStepKey,optional"`                      // 二次校验票据 key
	TwoStepValue string `json:"twoStepValue,optional"`                    // 二次校验票据 value
}

// Validate 校验重置首次登录状态请求。
func (r *ResetAdminInitialStateReq) Validate() error {
	if r.ID <= 0 {
		return errors.Errorf("管理员ID不能为空")
	}
	if err := validateAdminPasswordRequired(r.Password, "临时密码"); err != nil {
		return errors.Tag(err)
	}
	return nil
}

// InitAdminBootstrapReq 表示项目首次上线时通过内网接口初始化管理员账号的请求。
type InitAdminBootstrapReq struct {
	Username    string `json:"username"`             // 登录用户名
	Password    string `json:"password"`             // 首次登录临时密码
	RealName    string `json:"realName,optional"`    // 真实姓名
	Email       string `json:"email,optional"`       // 邮箱
	Phone       string `json:"phone,optional"`       // 电话
	Avatar      string `json:"avatar,optional"`      // 头像地址
	Description string `json:"description,optional"` // 备注说明
}

// Validate 校验内网初始化管理员请求。
func (r *InitAdminBootstrapReq) Validate() error {
	r.Username = strings.TrimSpace(r.Username)
	r.RealName = strings.TrimSpace(r.RealName)
	r.Email = strings.TrimSpace(r.Email)
	r.Phone = strings.TrimSpace(r.Phone)
	r.Avatar = strings.TrimSpace(r.Avatar)
	r.Description = strings.TrimSpace(r.Description)
	if r.Username == "" {
		return errors.Errorf("用户名不能为空")
	}
	if err := validateAdminPasswordRequired(r.Password, "临时密码"); err != nil {
		return errors.Tag(err)
	}
	return nil
}

// AdminRoleAssignReq 表示管理员角色分配请求。
type AdminRoleAssignReq struct {
	ID           int    `path:"id" json:"id,optional" form:"id,optional"`           // 管理员 ID
	RoleIDs      []int  `json:"roleIDs,optional"`                                   // 角色 ID 列表
	TwoStepKey   string `json:"twoStepKey,optional" form:"twoStepKey,optional"`     // MFA 二次校验票据 key
	TwoStepValue string `json:"twoStepValue,optional" form:"twoStepValue,optional"` // MFA 二次校验票据 value
}

// Validate 校验管理员角色分配请求。
func (r *AdminRoleAssignReq) Validate() error {
	if r.ID <= 0 {
		return errors.Errorf("管理员ID不能为空")
	}
	if len(UniquePositiveInts(r.RoleIDs)) == 0 {
		return errors.Errorf("角色ID不能为空")
	}
	return nil
}

// AdminRoleDeleteReq 表示解除管理员单个角色请求。
type AdminRoleDeleteReq struct {
	ID     int `path:"id" json:"id,optional" form:"id,optional"` // 管理员 ID
	RoleID int `json:"roleID,optional" form:"roleID,optional"`   // 角色 ID
}

// Validate 校验解除管理员角色请求。
func (r *AdminRoleDeleteReq) Validate() error {
	if r.ID <= 0 {
		return errors.Errorf("管理员ID不能为空")
	}
	if r.RoleID <= 0 {
		return errors.Errorf("角色ID不能为空")
	}
	return nil
}

// AdminItem 表示管理员列表项。
type AdminItem struct {
	ID                int             `json:"id"`                // 管理员 ID
	Username          string          `json:"username"`          // 登录用户名
	RealName          string          `json:"realName"`          // 真实姓名
	NeedResetPassword int             `json:"needResetPassword"` // 是否必须修改登录密码：0 否，1 是
	Email             string          `json:"email"`             // 邮箱
	Phone             string          `json:"phone"`             // 电话
	MfaStatus         int             `json:"mfaStatus"`         // MFA 状态：0 未启用，1 已启用
	Status            int             `json:"status"`            // 账号状态：1 正常，0 禁用
	Avatar            string          `json:"avatar"`            // 头像地址
	Description       string          `json:"description"`       // 备注说明
	LastLoginTime     string          `json:"lastLoginTime"`     // 最近登录时间
	LastLoginIP       string          `json:"lastLoginIP"`       // 最近登录 IP
	LastLoginIpaddr   string          `json:"lastLoginIpaddr"`   // 最近登录 IP 归属地
	RoleIDs           []int           `json:"roleIDs"`           // 已绑定角色 ID 列表
	Roles             []AdminRoleItem `json:"roles"`             // 已绑定角色列表
	CreatedAt         string          `json:"createdAt"`         // 创建时间
	UpdatedAt         string          `json:"updatedAt"`         // 更新时间
}

// AdminRoleListItem 表示管理员角色列表项。
type AdminRoleListItem struct {
	ID          int    `json:"id"`          // 角色 ID
	Title       string `json:"title"`       // 角色名称
	Status      int    `json:"status"`      // 角色状态
	Description string `json:"description"` // 角色描述
	CreatedAt   string `json:"createdAt"`   // 绑定时间
}

// InitAdminBootstrapResp 表示内网初始化管理员完成后的结果。
type InitAdminBootstrapResp struct {
	ID                int    `json:"id"`                // 管理员 ID
	Username          string `json:"username"`          // 登录用户名
	RoleID            int    `json:"roleID"`            // 当前绑定的超级管理员角色 ID
	NeedResetPassword int    `json:"needResetPassword"` // 是否必须修改登录密码：0 否，1 是
	MfaStatus         int    `json:"mfaStatus"`         // MFA 状态：0 未启用，1 已启用
	Status            int    `json:"status"`            // 账号状态：1 正常，0 禁用
	Operation         string `json:"operation"`         // 本次操作类型：当前固定为 reset，表示重置既有超级管理员账号
}
