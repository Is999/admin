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

// UpdateRoleReq 表示编辑角色基础资料请求。
type UpdateRoleReq struct {
	ID          int     `path:"id" json:"id,optional" form:"id,optional"` // 角色 ID，编辑时由路径注入
	Title       string  `json:"title"`                                    // 角色名称
	Pid         *int    `json:"pid"`                                      // 父级角色 ID，编辑时必须明确提交
	Description *string `json:"description"`                              // 角色描述，允许显式传空字符串清空
}

// CreateRoleReq 表示新增角色请求。
type CreateRoleReq struct {
	Title       string `json:"title"`           // 角色名称
	Pid         int    `json:"pid,optional"`    // 父级角色 ID
	Status      *int   `json:"status,optional"` // 角色状态：1 正常，0 禁用；新增未传默认正常
	Description string `json:"description"`     // 角色描述
}

// Validate 校验新增角色请求。
func (r *CreateRoleReq) Validate() error {
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

// Validate 校验编辑角色基础资料请求。
func (r *UpdateRoleReq) Validate() error {
	r.Title = strings.TrimSpace(r.Title)
	if r.ID <= 0 {
		return errors.Errorf("角色 ID不能为空")
	}
	if r.Title == "" {
		return errors.Errorf("角色名称不能为空")
	}
	if r.Pid == nil || *r.Pid < 0 {
		return errors.Errorf("父级角色 ID不合法")
	}
	if r.Description == nil {
		return errors.Errorf("必须提交角色描述")
	}
	description := strings.TrimSpace(*r.Description)
	r.Description = &description
	return nil
}

// ParentID 返回编辑请求明确提交的父级角色 ID。
func (r *UpdateRoleReq) ParentID() int {
	if r == nil || r.Pid == nil {
		return 0
	}
	return *r.Pid
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

// RolePermissionSaveReq 表示保存角色权限请求。
type RolePermissionSaveReq struct {
	ID                 int   `path:"id" json:"id,optional" form:"id,optional"` // 角色 ID
	RoutePermissionIDs []int `json:"routePermissionIds"`                       // 正常路由权限 ID 列表
	DocPermissionIDs   []int `json:"docPermissionIds"`                         // 文档权限 ID 列表
}

// Validate 校验保存角色权限请求。
func (r *RolePermissionSaveReq) Validate() error {
	if r.ID <= 0 {
		return errors.Errorf("角色 ID不能为空")
	}
	if r.RoutePermissionIDs == nil {
		return errors.Errorf("必须提交路由权限 ID列表")
	}
	if r.DocPermissionIDs == nil {
		return errors.Errorf("必须提交文档权限 ID列表")
	}
	for _, permissionID := range r.RoutePermissionIDs {
		if permissionID <= 0 {
			return errors.Errorf("路由权限 ID不合法")
		}
	}
	for _, permissionID := range r.DocPermissionIDs {
		if permissionID <= 0 {
			return errors.Errorf("文档权限 ID不合法")
		}
	}
	r.RoutePermissionIDs = UniquePositiveInts(r.RoutePermissionIDs)
	r.DocPermissionIDs = UniquePositiveInts(r.DocPermissionIDs)
	return nil
}

// RolePermissionTreeResp 表示角色的两类权限树数据。
type RolePermissionTreeResp struct {
	RoutePermissions []AdminPermissionItem    `json:"routePermissions"` // 正常路由权限树
	DocPermissions   []AdminDocPermissionItem `json:"docPermissions"`   // 文档权限列表
	Writable         bool                     `json:"writable"`         // 当前角色权限是否允许修改
}

// AdminDocPermissionItem 表示角色授权页中的单篇文档权限。
type AdminDocPermissionItem struct {
	ID              int    `json:"id"`              // 文档权限 ID
	Site            string `json:"site"`            // 文档站：admin 或 api
	Path            string `json:"path"`            // 站点内 Markdown 相对路径
	Title           string `json:"title"`           // 文档标题
	Description     string `json:"description"`     // 文档描述
	Status          int    `json:"status"`          // 文档权限状态
	Checked         bool   `json:"checked"`         // 当前角色是否已勾选
	Disabled        bool   `json:"disabled"`        // 当前节点是否禁用
	DisableCheckbox bool   `json:"disableCheckbox"` // 当前节点是否禁止勾选
	Selectable      bool   `json:"selectable"`      // 当前节点是否允许选择
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
	Manageable      bool            `json:"manageable"`      // 当前管理员是否有权管理该角色
	CanCreateChild  bool            `json:"canCreateChild"`  // 当前管理员是否可在该角色下新增子角色
	Children        []AdminRoleItem `json:"children"`        // 子角色列表
	CreatedAt       string          `json:"createdAt"`       // 创建时间
	UpdatedAt       string          `json:"updatedAt"`       // 更新时间
}
