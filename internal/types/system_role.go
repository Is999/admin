//lint:file-ignore SA5008 ignore go-zero optional tag

package types

import (
	"strings"

	"github.com/Is999/go-utils/errors"
)

// RoleListReq 表示角色列表查询请求。
type RoleListReq struct {
	Title       string `json:"title,optional" form:"title,optional"`               // 角色名称筛选
	Status      *int   `json:"status,optional" form:"status,optional"`             // 角色状态筛选
	Pid         *int   `json:"pid,optional" form:"pid,optional"`                   // 父级 ID 筛选
	IsGenealogy int    `json:"is_genealogy,optional" form:"is_genealogy,optional"` // 是否按族谱筛选：1 是，0 否
	GetOrderReq        // 复用排序参数
	GetPageReq         // 搜索接口分页参数；详情接口会忽略该字段
}

// Validate 校验角色列表请求。
func (r *RoleListReq) Validate() error {
	r.Title = strings.TrimSpace(r.Title)
	if r.Status != nil && (*r.Status != 0 && *r.Status != 1) {
		return errors.Errorf("角色状态不合法")
	}
	if err := r.GetOrderReq.Validate(); err != nil {
		return errors.Tag(err)
	}
	return r.GetPageReq.Validate()
}

// SaveRoleReq 表示新增或编辑角色请求。
type SaveRoleReq struct {
	ID          int    `path:"id" json:"id,optional" form:"id,optional"` // 角色 ID，编辑时由路径注入
	Title       string `json:"title"`                                    // 角色名称
	Pid         int    `json:"pid,optional"`                             // 父级角色 ID
	Status      *int   `json:"status,optional"`                          // 角色状态：1 正常，0 禁用；新增未传默认正常
	Description string `json:"description"`                              // 角色描述
	Permissions []int  `json:"permissions,optional"`                     // 角色权限 ID 列表
}

// CreateRoleReq 表示新增角色请求。
type CreateRoleReq struct {
	Title       string `json:"title"`                // 角色名称
	Pid         int    `json:"pid,optional"`         // 父级角色 ID
	Status      *int   `json:"status,optional"`      // 角色状态：1 正常，0 禁用；新增未传默认正常
	Description string `json:"description"`          // 角色描述
	Permissions []int  `json:"permissions,optional"` // 角色权限 ID 列表
}

// ToSaveRoleReq 转为统一的角色保存请求，便于复用创建逻辑。
func (r *CreateRoleReq) ToSaveRoleReq() *SaveRoleReq {
	if r == nil {
		return &SaveRoleReq{}
	}
	return &SaveRoleReq{
		Title:       r.Title,
		Pid:         r.Pid,
		Status:      r.Status,
		Description: r.Description,
		Permissions: r.Permissions,
	}
}

// Validate 校验新增角色请求。
func (r *CreateRoleReq) Validate() error {
	saveReq := r.ToSaveRoleReq()
	if err := saveReq.Validate(); err != nil {
		return errors.Tag(err)
	}
	if r != nil {
		// 将 SaveRoleReq 校验阶段产生的字段清洗结果回写，避免 handler 后续再次转换时丢失归一化值。
		r.Title = saveReq.Title
		r.Description = saveReq.Description
	}
	return nil
}

// Validate 校验新增或编辑角色请求。
func (r *SaveRoleReq) Validate() error {
	r.Title = strings.TrimSpace(r.Title)
	r.Description = strings.TrimSpace(r.Description)
	if r.Title == "" {
		return errors.Errorf("角色名称不能为空")
	}
	if r.Status != nil && (*r.Status != 0 && *r.Status != 1) {
		return errors.Errorf("角色状态不合法")
	}
	if r.Pid < 0 {
		return errors.Errorf("父级角色 ID不合法")
	}
	return nil
}

// RoleStatusReq 表示修改角色状态请求。
type RoleStatusReq struct {
	ID     int  `path:"id" json:"id,optional" form:"id,optional"` // 角色 ID
	Status *int `json:"status,optional" form:"status,optional"`   // 角色状态：1 正常，0 禁用
}

// StatusValue 返回归一化后的角色状态值。
func (r *RoleStatusReq) StatusValue() int {
	if r.Status == nil {
		return -1
	}
	return *r.Status
}

// Validate 校验角色状态请求。
func (r *RoleStatusReq) Validate() error {
	if r.ID <= 0 {
		return errors.Errorf("角色 ID不能为空")
	}
	if status := r.StatusValue(); status != 0 && status != 1 {
		return errors.Errorf("角色状态不合法")
	}
	return nil
}

// RolePermissionReq 表示角色权限查询请求。
type RolePermissionReq struct {
	ID    int    `path:"id" json:"id,optional" form:"id,optional"`          // 角色 ID
	IsPid string `path:"isPid" json:"isPid,optional" form:"isPid,optional"` // 是否按父级权限查询：y/n
}

// Validate 校验角色权限查询请求。
func (r *RolePermissionReq) Validate() error {
	if r.ID <= 0 {
		return errors.Errorf("角色 ID不能为空")
	}
	if r.IsPid == "" {
		r.IsPid = "n"
	}
	if r.IsPid != "y" && r.IsPid != "n" {
		return errors.Errorf("isPid参数不合法")
	}
	return nil
}

// RolePermissionSaveReq 表示保存角色权限请求。
type RolePermissionSaveReq struct {
	ID          int   `path:"id" json:"id,optional" form:"id,optional"` // 角色 ID
	Permissions []int `json:"permissions"`                              // 权限 ID 列表
}

// Validate 校验保存角色权限请求。
func (r *RolePermissionSaveReq) Validate() error {
	if r.ID <= 0 {
		return errors.Errorf("角色 ID不能为空")
	}
	r.Permissions = UniquePositiveInts(r.Permissions)
	return nil
}

// AdminRoleItem 表示角色响应项。
type AdminRoleItem struct {
	ID              int             `json:"id"`              // 角色 ID
	Title           string          `json:"title"`           // 角色名称
	Pid             int             `json:"pid"`             // 父级角色 ID
	Pids            string          `json:"pids"`            // 父级角色族谱
	Status          int             `json:"status"`          // 角色状态
	Description     string          `json:"description"`     // 角色描述
	IsDelete        int             `json:"isDelete"`        // 是否已删除
	Disabled        bool            `json:"disabled"`        // 当前节点是否禁用
	DisableCheckbox bool            `json:"disableCheckbox"` // 当前节点是否禁止勾选
	Selectable      bool            `json:"selectable"`      // 当前节点是否允许选择
	Permissions     []int           `json:"permissions"`     // 已绑定权限 ID 列表
	Children        []AdminRoleItem `json:"children"`        // 子角色列表
	CreatedAt       string          `json:"createdAt"`       // 创建时间
	UpdatedAt       string          `json:"updatedAt"`       // 更新时间
}
