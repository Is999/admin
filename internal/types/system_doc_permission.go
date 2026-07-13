//lint:file-ignore SA5008 ignore go-zero optional tag

package types

import (
	"strings"

	"github.com/Is999/go-utils/errors"
)

// DocPermissionListReq 表示文档权限列表查询请求。
type DocPermissionListReq struct {
	Site       string `json:"site,optional" form:"site,optional"`     // 文档站筛选：admin 或 api
	Title      string `json:"title,optional" form:"title,optional"`   // 文档标题筛选
	Path       string `json:"path,optional" form:"path,optional"`     // 文档相对路径筛选
	Status     *int   `json:"status,optional" form:"status,optional"` // 文档权限状态筛选
	GetPageReq        // 复用通用分页参数
}

// Validate 校验并归一化文档权限列表查询请求。
func (r *DocPermissionListReq) Validate() error {
	r.Site = strings.ToLower(strings.TrimSpace(r.Site))
	r.Title = strings.TrimSpace(r.Title)
	r.Path = strings.TrimSpace(r.Path)
	if r.Site != "" && r.Site != "admin" && r.Site != "api" {
		return errors.Errorf("文档站不合法")
	}
	if r.Status != nil && *r.Status != 0 && *r.Status != 1 {
		return errors.Errorf("文档权限状态不合法")
	}
	return r.GetPageReq.Validate()
}

// DocPermissionStatusReq 表示修改文档权限状态请求。
type DocPermissionStatusReq struct {
	ID     int  `path:"id" json:"id,optional" form:"id,optional"` // 文档权限 ID
	Status *int `json:"status" form:"status"`                     // 状态：1 启用，0 禁用
}

// StatusValue 返回请求明确提交的文档权限状态。
func (r *DocPermissionStatusReq) StatusValue() int {
	if r == nil || r.Status == nil {
		return -1
	}
	return *r.Status
}

// Validate 校验文档权限状态请求。
func (r *DocPermissionStatusReq) Validate() error {
	if r.ID <= 0 {
		return errors.Errorf("文档权限ID不能为空")
	}
	if status := r.StatusValue(); status != 0 && status != 1 {
		return errors.Errorf("文档权限状态不合法")
	}
	return nil
}

// AdminDocPermissionListItem 表示文档权限管理列表项。
type AdminDocPermissionListItem struct {
	ID          int    `json:"id"`          // 文档权限 ID
	Site        string `json:"site"`        // 文档站：admin 或 api
	Path        string `json:"path"`        // 文档站内 Markdown 相对路径
	Title       string `json:"title"`       // 文档标题
	Description string `json:"description"` // 文档描述
	Status      int    `json:"status"`      // 状态：1 启用，0 禁用
	CreatedAt   string `json:"createdAt"`   // 创建时间
	UpdatedAt   string `json:"updatedAt"`   // 更新时间
}
