//lint:file-ignore SA5008 ignore go-zero optional tag

package types

import (
	"strings"

	"github.com/Is999/go-utils/errors"
)

// PermissionListReq 表示权限列表查询请求。
type PermissionListReq struct {
	UUID        string `json:"uuid,optional" form:"uuid,optional"`                 // 权限 UUID 筛选
	Title       string `json:"title,optional" form:"title,optional"`               // 权限名称筛选
	Module      string `json:"module,optional" form:"module,optional"`             // 权限模块筛选
	Types       []int  `json:"type,optional" form:"type,optional"`                 // 权限类型筛选
	Status      *int   `json:"status,optional" form:"status,optional"`             // 权限状态筛选
	Pid         *int   `json:"pid,optional" form:"pid,optional"`                   // 父级 ID 筛选
	IsGenealogy int    `json:"is_genealogy,optional" form:"is_genealogy,optional"` // 是否按族谱筛选：1 是，0 否
	GetOrderReq        // 复用排序参数
	GetPageReq         // 复用分页参数
}

// Validate 校验权限列表查询请求。
func (r *PermissionListReq) Validate() error {
	r.UUID = strings.TrimSpace(r.UUID)
	r.Title = strings.TrimSpace(r.Title)
	r.Module = strings.TrimSpace(r.Module)
	if r.Status != nil && (*r.Status != 0 && *r.Status != 1) {
		return errors.Errorf("权限状态不合法")
	}
	for _, typ := range r.Types {
		if typ < 0 || typ > 8 {
			return errors.Errorf("权限类型不合法")
		}
	}
	if err := r.GetOrderReq.Validate(); err != nil {
		return errors.Tag(err)
	}
	return r.GetPageReq.Validate()
}

// SavePermissionReq 表示新增或编辑权限请求。
type SavePermissionReq struct {
	ID          int    `path:"id" json:"id,optional" form:"id,optional"` // 权限 ID，编辑时由路径注入
	UUID        string `json:"uuid,optional"`                            // 权限 UUID，新增时必填
	Title       string `json:"title"`                                    // 权限名称
	Module      string `json:"module,optional"`                          // 权限匹配模块
	Pid         int    `json:"pid,optional"`                             // 父级权限 ID
	Type        int    `json:"type"`                                     // 权限类型
	Description string `json:"description,optional"`                     // 权限描述
	Status      *int   `json:"status,optional"`                          // 权限状态：1 启用，0 禁用；新增未传默认启用
}

// CreatePermissionReq 表示新增权限请求。
type CreatePermissionReq struct {
	UUID        string `json:"uuid,optional"`        // 权限 UUID，新增时必填
	Title       string `json:"title"`                // 权限名称
	Module      string `json:"module,optional"`      // 权限匹配模块
	Pid         int    `json:"pid,optional"`         // 父级权限 ID
	Type        int    `json:"type"`                 // 权限类型
	Description string `json:"description,optional"` // 权限描述
	Status      *int   `json:"status,optional"`      // 权限状态：1 启用，0 禁用；新增未传默认启用
}

// ToSavePermissionReq 转为统一的权限保存请求，便于复用创建逻辑。
func (r *CreatePermissionReq) ToSavePermissionReq() *SavePermissionReq {
	if r == nil {
		return &SavePermissionReq{}
	}
	return &SavePermissionReq{
		UUID:        r.UUID,
		Title:       r.Title,
		Module:      r.Module,
		Pid:         r.Pid,
		Type:        r.Type,
		Description: r.Description,
		Status:      r.Status,
	}
}

// Validate 校验新增权限请求。
func (r *CreatePermissionReq) Validate() error {
	saveReq := r.ToSavePermissionReq()
	if err := saveReq.Validate(); err != nil {
		return errors.Tag(err)
	}
	if r != nil {
		// 将 SavePermissionReq 校验阶段产生的字段清洗结果回写，保证权限模块和 UUID 后续落库/缓存使用同一份干净值。
		r.UUID = saveReq.UUID
		r.Title = saveReq.Title
		r.Module = saveReq.Module
		r.Description = saveReq.Description
	}
	return nil
}

// Validate 校验新增或编辑权限请求。
func (r *SavePermissionReq) Validate() error {
	r.UUID = strings.TrimSpace(r.UUID)
	r.Title = strings.TrimSpace(r.Title)
	r.Module = strings.TrimSpace(r.Module)
	r.Description = strings.TrimSpace(r.Description)
	if r.Title == "" {
		return errors.Errorf("权限名称不能为空")
	}
	if r.Type < 0 || r.Type > 8 {
		return errors.Errorf("权限类型不合法")
	}
	if r.Status != nil && (*r.Status != 0 && *r.Status != 1) {
		return errors.Errorf("权限状态不合法")
	}
	if r.Pid < 0 {
		return errors.Errorf("父级权限ID不合法")
	}
	return nil
}

// PermissionStatusReq 表示修改权限状态请求。
type PermissionStatusReq struct {
	ID     int  `path:"id" json:"id,optional" form:"id,optional"` // 权限 ID
	Status *int `json:"status,optional" form:"status,optional"`   // 权限状态：1 启用，0 禁用
}

// StatusValue 返回归一化后的权限状态值。
func (r *PermissionStatusReq) StatusValue() int {
	if r.Status == nil {
		return -1
	}
	return *r.Status
}

// Validate 校验权限状态请求。
func (r *PermissionStatusReq) Validate() error {
	if r.ID <= 0 {
		return errors.Errorf("权限ID不能为空")
	}
	if status := r.StatusValue(); status != 0 && status != 1 {
		return errors.Errorf("权限状态不合法")
	}
	return nil
}

// AdminPermissionItem 表示权限响应项。
type AdminPermissionItem struct {
	ID              int                   `json:"id"`              // 权限 ID
	UUID            string                `json:"uuid"`            // 权限 UUID
	Title           string                `json:"title"`           // 权限名称
	Module          string                `json:"module"`          // 权限匹配模块
	Pid             int                   `json:"pid"`             // 父级权限 ID
	Pids            string                `json:"pids"`            // 父级权限族谱
	Type            int                   `json:"type"`            // 权限类型
	Description     string                `json:"description"`     // 权限描述
	Status          int                   `json:"status"`          // 权限状态
	Checked         bool                  `json:"checked"`         // 当前角色是否已勾选
	Disabled        bool                  `json:"disabled"`        // 当前节点是否禁用
	DisableCheckbox bool                  `json:"disableCheckbox"` // 当前节点是否禁止勾选
	Selectable      bool                  `json:"selectable"`      // 当前节点是否允许选择
	HasChild        bool                  `json:"hasChild"`        // 是否存在子权限，供前端树表格按需懒加载
	Children        []AdminPermissionItem `json:"children"`        // 子权限列表
	CreatedAt       string                `json:"createdAt"`       // 创建时间
	UpdatedAt       string                `json:"updatedAt"`       // 更新时间
}

// PermissionMaxUUIDResp 表示权限最大 UUID 响应。
type PermissionMaxUUIDResp struct {
	UUID string `json:"uuid"` // 下一个可用权限 UUID
}
