//lint:file-ignore SA5008 ignore go-zero optional tag

package types

import (
	"encoding/json"
	"strings"

	"github.com/Is999/go-utils/errors"
)

// SysConfigListReq 表示系统常量配置列表查询请求。
type SysConfigListReq struct {
	UUID        string `json:"uuid,optional" form:"uuid,optional"`         // 配置 UUID 筛选
	Title       string `json:"title,optional" form:"title,optional"`       // 配置标题筛选
	PagePath    string `json:"pagePath,optional" form:"pagePath,optional"` // 页面路径筛选
	GetOrderReq        // 复用排序参数
	GetPageReq         // 复用分页参数
}

// Validate 校验系统常量配置列表请求。
func (r *SysConfigListReq) Validate() error {
	r.UUID = strings.TrimSpace(r.UUID)
	r.Title = strings.TrimSpace(r.Title)
	r.PagePath = strings.TrimSpace(r.PagePath)
	if err := r.GetOrderReq.Validate(); err != nil {
		return errors.Tag(err)
	}
	return r.GetPageReq.Validate()
}

// SaveSysConfigReq 表示新增或编辑系统常量配置请求。
type SaveSysConfigReq struct {
	ID      int    `path:"id" json:"id,optional" form:"id,optional"` // 配置 ID，编辑时由路径注入
	UUID    string `json:"uuid"`                                     // 配置 UUID
	Title   string `json:"title"`                                    // 配置标题
	Type    int    `json:"type"`                                     // 配置类型
	Value   any    `json:"value"`                                    // 配置值，先接收任意 JSON，再统一归一化
	Example any    `json:"example,optional"`                         // 配置示例，先接收任意 JSON，再统一归一化
	Remark  string `json:"remark,optional"`                          // 备注
	Page    string `json:"page,optional"`                            // 页面路径
	Pid     int    `json:"pid,optional"`                             // 上级配置 ID
	Version *int   `json:"version,optional"`                         // 乐观锁版本号，编辑时必传
}

// CreateSysConfigReq 表示新增系统常量配置请求。
type CreateSysConfigReq struct {
	UUID    string `json:"uuid"`             // 配置 UUID
	Title   string `json:"title"`            // 配置标题
	Type    int    `json:"type"`             // 配置类型
	Value   any    `json:"value"`            // 配置值，先接收任意 JSON，再统一归一化
	Example any    `json:"example,optional"` // 配置示例，先接收任意 JSON，再统一归一化
	Remark  string `json:"remark,optional"`  // 备注
	Page    string `json:"page,optional"`    // 页面路径
	Pid     int    `json:"pid,optional"`     // 上级配置 ID
}

// ToSaveSysConfigReq 转为统一的系统配置保存请求，便于复用创建逻辑。
func (r *CreateSysConfigReq) ToSaveSysConfigReq() *SaveSysConfigReq {
	if r == nil {
		return &SaveSysConfigReq{}
	}
	return &SaveSysConfigReq{
		UUID:    r.UUID,
		Title:   r.Title,
		Type:    r.Type,
		Value:   r.Value,
		Example: r.Example,
		Remark:  r.Remark,
		Page:    r.Page,
		Pid:     r.Pid,
	}
}

// Validate 校验新增系统常量配置请求。
func (r *CreateSysConfigReq) Validate() error {
	saveReq := r.ToSaveSysConfigReq()
	if err := saveReq.Validate(); err != nil {
		return errors.Tag(err)
	}
	if r != nil {
		// 将 SaveSysConfigReq 校验阶段产生的字段清洗结果回写，避免配置 UUID、页面路径和缓存 key 出现首尾空白。
		r.UUID = saveReq.UUID
		r.Title = saveReq.Title
		r.Remark = saveReq.Remark
		r.Page = saveReq.Page
	}
	return nil
}

// Validate 校验新增或编辑系统常量配置请求。
func (r *SaveSysConfigReq) Validate() error {
	r.UUID = strings.TrimSpace(r.UUID)
	r.Title = strings.TrimSpace(r.Title)
	r.Remark = strings.TrimSpace(r.Remark)
	r.Page = strings.TrimSpace(r.Page)
	if r.UUID == "" {
		return errors.Errorf("配置UUID不能为空")
	}
	if r.Title == "" {
		return errors.Errorf("配置标题不能为空")
	}
	if r.Type < 0 || r.Type > 6 {
		return errors.Errorf("配置类型不合法")
	}
	if _, err := normalizeRequestJSONValue(r.Value, false); err != nil {
		return errors.Errorf("配置值不能为空")
	}
	if r.Pid < 0 {
		return errors.Errorf("上级配置ID不合法")
	}
	if r.Version != nil && *r.Version < 0 {
		return errors.Errorf("配置版本不合法")
	}
	if r.ID > 0 && r.Version == nil {
		return errors.Errorf("配置版本不能为空")
	}
	return nil
}

// ValueRawMessage 返回归一化后的配置值原始 JSON。
func (r *SaveSysConfigReq) ValueRawMessage() (json.RawMessage, error) {
	return normalizeRequestJSONValue(r.Value, false)
}

// ExampleRawMessage 返回归一化后的配置示例原始 JSON；未传时返回 nil。
func (r *SaveSysConfigReq) ExampleRawMessage() (json.RawMessage, error) {
	return normalizeRequestJSONValue(r.Example, true)
}

// SysConfigItem 表示系统常量配置响应项。
type SysConfigItem struct {
	ID        int             `json:"id"`        // 配置 ID
	UUID      string          `json:"uuid"`      // 配置 UUID
	Title     string          `json:"title"`     // 配置标题
	Type      int             `json:"type"`      // 配置类型
	Value     json.RawMessage `json:"value"`     // 配置值
	Example   json.RawMessage `json:"example"`   // 配置示例
	Remark    string          `json:"remark"`    // 备注
	Page      string          `json:"page"`      // 页面路径
	Pid       int             `json:"pid"`       // 上级配置 ID
	Pids      string          `json:"pids"`      // 上级配置族谱
	Version   int             `json:"version"`   // 配置版本
	Editable  int             `json:"editable"`  // 是否允许编辑：1 是，0 否
	CreatedAt string          `json:"createdAt"` // 创建时间
	UpdatedAt string          `json:"updatedAt"` // 更新时间
}

// SysConfigExcelExportReq 表示字典配置 Excel 导出筛选请求。
type SysConfigExcelExportReq struct {
	UUID     string `json:"uuid,optional" form:"uuid,optional"`         // 配置 UUID 筛选
	Title    string `json:"title,optional" form:"title,optional"`       // 配置标题筛选
	PagePath string `json:"pagePath,optional" form:"pagePath,optional"` // 页面路径筛选
}

// Validate 校验字典配置 Excel 导出筛选请求。
func (r *SysConfigExcelExportReq) Validate() error {
	r.UUID = strings.TrimSpace(r.UUID)
	r.Title = strings.TrimSpace(r.Title)
	r.PagePath = strings.TrimSpace(r.PagePath)
	return nil
}

// SysConfigExcelImportReq 表示字典配置 Excel 导入请求。
type SysConfigExcelImportReq struct {
	UploadID string `json:"uploadId,optional" form:"uploadId,optional"` // 已上传的 Excel 文件会话 ID
	FileURL  string `json:"fileUrl,optional" form:"fileUrl,optional"`   // 已持久化的统一存储文件 URL（支持 S3 URL）
}

// Validate 校验字典配置 Excel 导入请求。
func (r *SysConfigExcelImportReq) Validate() error {
	r.UploadID = strings.TrimSpace(r.UploadID)
	r.FileURL = strings.TrimSpace(r.FileURL)
	if r.UploadID == "" && r.FileURL == "" {
		return errors.Errorf("uploadId 或 fileUrl 不能为空")
	}
	return nil
}

// SysConfigExcelImportResp 表示字典配置 Excel 导入结果。
type SysConfigExcelImportResp struct {
	Created     int  `json:"created"`     // 新增数量
	Updated     int  `json:"updated"`     // 更新数量
	Skipped     int  `json:"skipped"`     // 跳过数量
	SyncPending bool `json:"syncPending"` // 是否仍需手动刷新配置缓存
}
