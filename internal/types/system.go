//lint:file-ignore SA5008 ignore go-zero optional tag

package types

import (
	"bytes"
	"encoding/json"

	"strconv"
	"strings"

	utils "github.com/Is999/go-utils"
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
	IsExtends   int    `json:"is_extends,optional"`                      // 是否继承同步子级权限，当前作为兼容字段保留
}

// CreateRoleReq 表示新增角色请求。
type CreateRoleReq struct {
	Title       string `json:"title"`                // 角色名称
	Pid         int    `json:"pid,optional"`         // 父级角色 ID
	Status      *int   `json:"status,optional"`      // 角色状态：1 正常，0 禁用；新增未传默认正常
	Description string `json:"description"`          // 角色描述
	Permissions []int  `json:"permissions,optional"` // 角色权限 ID 列表
	IsExtends   int    `json:"is_extends,optional"`  // 是否继承同步子级权限，当前作为兼容字段保留
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
		IsExtends:   r.IsExtends,
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
		return errors.Errorf("父级角色ID不合法")
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
		return errors.Errorf("角色ID不能为空")
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
		return errors.Errorf("角色ID不能为空")
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
	IsExtends   int   `json:"is_extends,optional"`                      // 是否继承同步子级权限，当前作为兼容字段保留
}

// Validate 校验保存角色权限请求。
func (r *RolePermissionSaveReq) Validate() error {
	if r.ID <= 0 {
		return errors.Errorf("角色ID不能为空")
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
	Children        []AdminPermissionItem `json:"children"`        // 子权限列表
	CreatedAt       string                `json:"createdAt"`       // 创建时间
	UpdatedAt       string                `json:"updatedAt"`       // 更新时间
}

// PermissionMaxUUIDResp 表示权限最大 UUID 响应。
type PermissionMaxUUIDResp struct {
	UUID string `json:"uuid"` // 下一个可用权限 UUID
}

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
	Created int `json:"created"` // 新增数量
	Updated int `json:"updated"` // 更新数量
	Skipped int `json:"skipped"` // 跳过数量
}

// SecretKeyListReq 表示秘钥管理列表查询请求。
type SecretKeyListReq struct {
	UUID          string `json:"uuid,optional" form:"uuid,optional"`                   // AppID / API KEY 唯一标识筛选
	Title         string `json:"title,optional" form:"title,optional"`                 // 秘钥标题筛选
	Status        *int   `json:"status,optional" form:"status,optional"`               // 应用状态筛选：1 启用，0 禁用
	SignStatus    *int   `json:"signStatus,optional" form:"signStatus,optional"`       // 签名验签状态筛选：1 启用，0 停用
	CryptoStatus  *int   `json:"cryptoStatus,optional" form:"cryptoStatus,optional"`   // 加密解密状态筛选：1 启用，0 停用
	StableVersion string `json:"stableVersion,optional" form:"stableVersion,optional"` // 稳定版本筛选
	GetOrderReq          // 复用排序参数
	GetPageReq           // 复用分页参数
}

// Validate 校验秘钥管理列表查询请求。
func (r *SecretKeyListReq) Validate() error {
	r.UUID = strings.TrimSpace(r.UUID)
	r.Title = strings.TrimSpace(r.Title)
	r.StableVersion = strings.TrimSpace(r.StableVersion)
	if r.Status != nil && (*r.Status != 0 && *r.Status != 1) {
		return errors.Errorf("秘钥状态不合法")
	}
	if r.SignStatus != nil && (*r.SignStatus != 0 && *r.SignStatus != 1) {
		return errors.Errorf("签名验签状态不合法")
	}
	if r.CryptoStatus != nil && (*r.CryptoStatus != 0 && *r.CryptoStatus != 1) {
		return errors.Errorf("加密解密状态不合法")
	}
	if err := r.GetOrderReq.Validate(); err != nil {
		return errors.Tag(err)
	}
	return r.GetPageReq.Validate()
}

// SaveSecretKeyReq 表示新增或编辑秘钥请求。
type SaveSecretKeyReq struct {
	ID                     int    `path:"id" json:"id,optional" form:"id,optional"`           // 秘钥 ID，编辑时由路径注入
	UUID                   string `json:"uuid"`                                               // AppID / API KEY 唯一标识
	Title                  string `json:"title"`                                              // 秘钥标题
	KeyVersion             string `json:"keyVersion"`                                         // 当前编辑的秘钥版本号
	AESKeyRef              string `json:"aesKeyRef,optional"`                                 // AES KEY 文件绝对路径
	AESIVRef               string `json:"aesIvRef,optional"`                                  // AES IV 文件绝对路径
	RSAPublicKeyUserRef    string `json:"rsaPublicKeyUserRef,optional"`                       // 用户 RSA 公钥文件绝对路径
	RSAPublicKeyServerRef  string `json:"rsaPublicKeyServerRef,optional"`                     // 服务端 RSA 公钥路径，可为空并由私钥派生
	RSAPrivateKeyServerRef string `json:"rsaPrivateKeyServerRef,optional"`                    // 服务端 RSA 私钥文件绝对路径
	Status                 int    `json:"status"`                                             // 应用状态：1 启用，0 禁用
	SignStatus             int    `json:"signStatus"`                                         // 签名验签状态：1 启用，0 停用
	CryptoStatus           int    `json:"cryptoStatus"`                                       // 加密解密状态：1 启用，0 停用
	VersionStatus          int    `json:"versionStatus"`                                      // 当前版本状态：1 启用，0 停用
	StableVersion          string `json:"stableVersion,optional"`                             // 当前稳定版本
	GrayVersion            string `json:"grayVersion,optional"`                               // 当前灰度版本
	GrayPercent            int    `json:"grayPercent,optional"`                               // 当前灰度流量百分比
	Remark                 string `json:"remark,optional"`                                    // 备注说明
	TwoStepKey             string `json:"twoStepKey,optional" form:"twoStepKey,optional"`     // MFA 二次校验票据 key
	TwoStepValue           string `json:"twoStepValue,optional" form:"twoStepValue,optional"` // MFA 二次校验票据 value
}

// SecretKeyVersionItem 表示秘钥版本响应项。
type SecretKeyVersionItem struct {
	ID                     int    `json:"id"`                     // 版本 ID
	KeyVersion             string `json:"keyVersion"`             // 版本号
	AESKeyRef              string `json:"aesKeyRef"`              // AES KEY 文件绝对路径
	AESIVRef               string `json:"aesIvRef"`               // AES IV 文件绝对路径
	RSAPublicKeyUserRef    string `json:"rsaPublicKeyUserRef"`    // 用户 RSA 公钥文件绝对路径
	RSAPublicKeyServerRef  string `json:"rsaPublicKeyServerRef"`  // 服务端 RSA 公钥路径，可为空并由私钥派生
	RSAPrivateKeyServerRef string `json:"rsaPrivateKeyServerRef"` // 服务端 RSA 私钥文件绝对路径
	SecretMasked           bool   `json:"secretMasked"`           // 当前响应中的敏感字段是否已脱敏
	Status                 int    `json:"status"`                 // 版本状态：1启用，0停用
	IsStable               bool   `json:"isStable"`               // 是否为稳定版本
	IsGray                 bool   `json:"isGray"`                 // 是否为灰度版本
	Remark                 string `json:"remark"`                 // 版本备注
	CreatedAt              string `json:"createdAt"`              // 创建时间
	UpdatedAt              string `json:"updatedAt"`              // 更新时间
}

// SecretKeyCheckItem 表示单个秘钥校验项结果。
type SecretKeyCheckItem struct {
	Key     string `json:"key"`     // 校验项唯一标识
	Label   string `json:"label"`   // 校验项中文标题
	Passed  bool   `json:"passed"`  // 当前校验项是否通过
	Level   string `json:"level"`   // 结果级别：success/warning/error
	Message string `json:"message"` // 校验结果说明
}

// SecretKeyCheckResult 表示秘钥预检或自检的聚合结果。
type SecretKeyCheckResult struct {
	UUID           string               `json:"uuid"`           // AppID / API KEY 唯一标识
	Title          string               `json:"title"`          // 秘钥标题
	KeyVersion     string               `json:"keyVersion"`     // 当前校验的秘钥版本号
	Mode           string               `json:"mode"`           // 校验模式：validate/self_check
	Status         int                  `json:"status"`         // 当前记录状态：1 启用，0 禁用
	AllPassed      bool                 `json:"allPassed"`      // 所有校验项是否全部通过
	CanSave        bool                 `json:"canSave"`        // 当前配置是否允许保存
	CanEnable      bool                 `json:"canEnable"`      // 当前配置是否允许启用
	RuntimeChecked bool                 `json:"runtimeChecked"` // 是否执行了运行态自检
	CacheRefreshed bool                 `json:"cacheRefreshed"` // 是否成功刷新秘钥缓存
	CheckedAt      string               `json:"checkedAt"`      // 校验完成时间
	DurationMs     int64                `json:"durationMs"`     // 校验耗时毫秒
	Items          []SecretKeyCheckItem `json:"items"`          // 分项校验结果
}

// SecretKeyValidateReq 表示秘钥路径预检请求。
type SecretKeyValidateReq struct {
	UUID                   string `json:"uuid"`                                               // AppID / API KEY 唯一标识
	Title                  string `json:"title"`                                              // 秘钥标题
	KeyVersion             string `json:"keyVersion"`                                         // 目标秘钥版本号
	AESKeyRef              string `json:"aesKeyRef,optional"`                                 // AES KEY 文件绝对路径
	AESIVRef               string `json:"aesIvRef,optional"`                                  // AES IV 文件绝对路径
	RSAPublicKeyUserRef    string `json:"rsaPublicKeyUserRef,optional"`                       // 用户 RSA 公钥文件绝对路径
	RSAPublicKeyServerRef  string `json:"rsaPublicKeyServerRef,optional"`                     // 服务端 RSA 公钥路径，可为空并由私钥派生
	RSAPrivateKeyServerRef string `json:"rsaPrivateKeyServerRef,optional"`                    // 服务端 RSA 私钥文件绝对路径
	Status                 int    `json:"status"`                                             // 状态：1 启用，0 禁用
	SignStatus             int    `json:"signStatus"`                                         // 签名验签状态：1 启用，0 停用
	CryptoStatus           int    `json:"cryptoStatus"`                                       // 加密解密状态：1 启用，0 停用
	VersionStatus          int    `json:"versionStatus,optional"`                             // 当前版本状态：1 启用，0 停用
	StableVersion          string `json:"stableVersion,optional"`                             // 当前稳定版本
	GrayVersion            string `json:"grayVersion,optional"`                               // 当前灰度版本
	GrayPercent            int    `json:"grayPercent,optional"`                               // 当前灰度流量百分比
	TwoStepKey             string `json:"twoStepKey,optional" form:"twoStepKey,optional"`     // MFA 二次校验票据 key
	TwoStepValue           string `json:"twoStepValue,optional" form:"twoStepValue,optional"` // MFA 二次校验票据 value
}

// ToSaveSecretKeyReq 转为统一的秘钥保存请求，便于复用字段清洗与基础校验。
func (r *SecretKeyValidateReq) ToSaveSecretKeyReq() *SaveSecretKeyReq {
	if r == nil {
		return &SaveSecretKeyReq{}
	}
	return &SaveSecretKeyReq{
		UUID:                   r.UUID,
		Title:                  r.Title,
		KeyVersion:             r.KeyVersion,
		AESKeyRef:              r.AESKeyRef,
		AESIVRef:               r.AESIVRef,
		RSAPublicKeyUserRef:    r.RSAPublicKeyUserRef,
		RSAPublicKeyServerRef:  r.RSAPublicKeyServerRef,
		RSAPrivateKeyServerRef: r.RSAPrivateKeyServerRef,
		Status:                 r.Status,
		SignStatus:             r.SignStatus,
		CryptoStatus:           r.CryptoStatus,
		VersionStatus:          r.VersionStatus,
		StableVersion:          r.StableVersion,
		GrayVersion:            r.GrayVersion,
		GrayPercent:            r.GrayPercent,
		TwoStepKey:             r.TwoStepKey,
		TwoStepValue:           r.TwoStepValue,
	}
}

// Validate 校验秘钥路径预检请求。
func (r *SecretKeyValidateReq) Validate() error {
	saveReq := r.ToSaveSecretKeyReq()
	if err := saveReq.Validate(); err != nil {
		return errors.Tag(err)
	}
	if r != nil {
		// 将 SaveSecretKeyReq 校验阶段产生的字段清洗和默认稳定版本回写，确保预检结果与实际保存链路一致。
		r.UUID = saveReq.UUID
		r.Title = saveReq.Title
		r.KeyVersion = saveReq.KeyVersion
		r.AESKeyRef = saveReq.AESKeyRef
		r.AESIVRef = saveReq.AESIVRef
		r.RSAPublicKeyUserRef = saveReq.RSAPublicKeyUserRef
		r.RSAPublicKeyServerRef = saveReq.RSAPublicKeyServerRef
		r.RSAPrivateKeyServerRef = saveReq.RSAPrivateKeyServerRef
		r.StableVersion = saveReq.StableVersion
		r.GrayVersion = saveReq.GrayVersion
	}
	return nil
}

// CreateSecretKeyReq 表示新增秘钥请求。
type CreateSecretKeyReq struct {
	UUID                   string `json:"uuid"`                                               // AppID / API KEY 唯一标识
	Title                  string `json:"title"`                                              // 秘钥标题
	KeyVersion             string `json:"keyVersion"`                                         // 当前编辑的秘钥版本号
	AESKeyRef              string `json:"aesKeyRef,optional"`                                 // AES KEY 文件绝对路径
	AESIVRef               string `json:"aesIvRef,optional"`                                  // AES IV 文件绝对路径
	RSAPublicKeyUserRef    string `json:"rsaPublicKeyUserRef,optional"`                       // 用户 RSA 公钥文件绝对路径
	RSAPublicKeyServerRef  string `json:"rsaPublicKeyServerRef,optional"`                     // 服务端 RSA 公钥路径，可为空并由私钥派生
	RSAPrivateKeyServerRef string `json:"rsaPrivateKeyServerRef,optional"`                    // 服务端 RSA 私钥文件绝对路径
	Status                 int    `json:"status"`                                             // 状态：1 启用，0 禁用
	SignStatus             int    `json:"signStatus"`                                         // 签名验签状态：1 启用，0 停用
	CryptoStatus           int    `json:"cryptoStatus"`                                       // 加密解密状态：1 启用，0 停用
	VersionStatus          int    `json:"versionStatus"`                                      // 当前版本状态：1 启用，0 停用
	StableVersion          string `json:"stableVersion,optional"`                             // 当前稳定版本
	GrayVersion            string `json:"grayVersion,optional"`                               // 当前灰度版本
	GrayPercent            int    `json:"grayPercent,optional"`                               // 当前灰度流量百分比
	Remark                 string `json:"remark,optional"`                                    // 备注说明
	TwoStepKey             string `json:"twoStepKey,optional" form:"twoStepKey,optional"`     // MFA 二次校验票据 key
	TwoStepValue           string `json:"twoStepValue,optional" form:"twoStepValue,optional"` // MFA 二次校验票据 value
}

// ToSaveSecretKeyReq 转为统一的秘钥保存请求，便于复用创建逻辑。
func (r *CreateSecretKeyReq) ToSaveSecretKeyReq() *SaveSecretKeyReq {
	if r == nil {
		return &SaveSecretKeyReq{}
	}
	return &SaveSecretKeyReq{
		UUID:                   r.UUID,
		Title:                  r.Title,
		KeyVersion:             r.KeyVersion,
		AESKeyRef:              r.AESKeyRef,
		AESIVRef:               r.AESIVRef,
		RSAPublicKeyUserRef:    r.RSAPublicKeyUserRef,
		RSAPublicKeyServerRef:  r.RSAPublicKeyServerRef,
		RSAPrivateKeyServerRef: r.RSAPrivateKeyServerRef,
		Status:                 r.Status,
		SignStatus:             r.SignStatus,
		CryptoStatus:           r.CryptoStatus,
		VersionStatus:          r.VersionStatus,
		StableVersion:          r.StableVersion,
		GrayVersion:            r.GrayVersion,
		GrayPercent:            r.GrayPercent,
		Remark:                 r.Remark,
		TwoStepKey:             r.TwoStepKey,
		TwoStepValue:           r.TwoStepValue,
	}
}

// Validate 校验新增秘钥请求。
func (r *CreateSecretKeyReq) Validate() error {
	saveReq := r.ToSaveSecretKeyReq()
	if err := saveReq.Validate(); err != nil {
		return errors.Tag(err)
	}
	if r != nil {
		// 回写校验阶段的字段清洗结果和默认稳定版本。
		r.UUID = saveReq.UUID
		r.Title = saveReq.Title
		r.KeyVersion = saveReq.KeyVersion
		r.AESKeyRef = saveReq.AESKeyRef
		r.AESIVRef = saveReq.AESIVRef
		r.RSAPublicKeyUserRef = saveReq.RSAPublicKeyUserRef
		r.RSAPublicKeyServerRef = saveReq.RSAPublicKeyServerRef
		r.RSAPrivateKeyServerRef = saveReq.RSAPrivateKeyServerRef
		r.StableVersion = saveReq.StableVersion
		r.GrayVersion = saveReq.GrayVersion
		r.Remark = saveReq.Remark
	}
	return nil
}

// Validate 校验新增或编辑秘钥请求。
func (r *SaveSecretKeyReq) Validate() error {
	r.UUID = strings.TrimSpace(r.UUID)
	r.Title = strings.TrimSpace(r.Title)
	r.KeyVersion = strings.TrimSpace(r.KeyVersion)
	r.AESKeyRef = strings.TrimSpace(r.AESKeyRef)
	r.AESIVRef = strings.TrimSpace(r.AESIVRef)
	r.RSAPublicKeyUserRef = strings.TrimSpace(r.RSAPublicKeyUserRef)
	r.RSAPublicKeyServerRef = strings.TrimSpace(r.RSAPublicKeyServerRef)
	r.RSAPrivateKeyServerRef = strings.TrimSpace(r.RSAPrivateKeyServerRef)
	r.StableVersion = strings.TrimSpace(r.StableVersion)
	r.GrayVersion = strings.TrimSpace(r.GrayVersion)
	r.Remark = strings.TrimSpace(r.Remark)
	if r.UUID == "" {
		return errors.Errorf("秘钥标识不能为空")
	}
	if len(r.UUID) > 64 {
		return errors.Errorf("秘钥标识长度不能超过64位")
	}
	if r.Title == "" {
		return errors.Errorf("秘钥标题不能为空")
	}
	if r.KeyVersion == "" {
		return errors.Errorf("秘钥版本不能为空")
	}
	if r.Status != 0 && r.Status != 1 {
		return errors.Errorf("秘钥状态不合法")
	}
	if r.SignStatus != 0 && r.SignStatus != 1 {
		return errors.Errorf("签名验签状态不合法")
	}
	if r.CryptoStatus != 0 && r.CryptoStatus != 1 {
		return errors.Errorf("加密解密状态不合法")
	}
	if r.VersionStatus != 0 && r.VersionStatus != 1 {
		return errors.Errorf("秘钥版本状态不合法")
	}
	if r.GrayPercent < 0 || r.GrayPercent > 100 {
		return errors.Errorf("灰度流量百分比必须在0到100之间")
	}
	if r.StableVersion == "" {
		r.StableVersion = r.KeyVersion
	}
	return nil
}

// SecretKeyStatusReq 表示修改秘钥状态请求。
type SecretKeyStatusReq struct {
	ID           int    `path:"id" json:"id,optional" form:"id,optional"`           // 秘钥 ID
	Status       *int   `json:"status,optional" form:"status,optional"`             // 状态：1 启用，0 禁用
	TwoStepKey   string `json:"twoStepKey,optional" form:"twoStepKey,optional"`     // MFA 二次校验票据 key
	TwoStepValue string `json:"twoStepValue,optional" form:"twoStepValue,optional"` // MFA 二次校验票据 value
}

// StatusValue 返回归一化后的秘钥状态值。
func (r *SecretKeyStatusReq) StatusValue() int {
	if r.Status == nil {
		return -1
	}
	return *r.Status
}

// Validate 校验修改秘钥状态请求。
func (r *SecretKeyStatusReq) Validate() error {
	if r.ID <= 0 {
		return errors.Errorf("秘钥ID不能为空")
	}
	if status := r.StatusValue(); status != 0 && status != 1 {
		return errors.Errorf("秘钥状态不合法")
	}
	return nil
}

// SecretKeyRenewReq 表示刷新秘钥缓存请求。
type SecretKeyRenewReq struct {
	UUID         string `path:"uuid" json:"uuid,optional" form:"uuid,optional"`     // AppID / API KEY 唯一标识
	TwoStepKey   string `json:"twoStepKey,optional" form:"twoStepKey,optional"`     // MFA 二次校验票据 key
	TwoStepValue string `json:"twoStepValue,optional" form:"twoStepValue,optional"` // MFA 二次校验票据 value
}

// Validate 校验刷新秘钥缓存请求。
func (r *SecretKeyRenewReq) Validate() error {
	r.UUID = strings.TrimSpace(r.UUID)
	if r.UUID == "" {
		return errors.Errorf("秘钥标识不能为空")
	}
	return nil
}

// SecretKeySelfCheckReq 表示秘钥自检请求。
type SecretKeySelfCheckReq struct {
	UUID         string `path:"uuid" json:"uuid,optional" form:"uuid,optional"`     // AppID / API KEY 唯一标识
	KeyVersion   string `json:"keyVersion,optional" form:"keyVersion,optional"`     // 目标秘钥版本号，默认稳定版本
	TwoStepKey   string `json:"twoStepKey,optional" form:"twoStepKey,optional"`     // MFA 二次校验票据 key
	TwoStepValue string `json:"twoStepValue,optional" form:"twoStepValue,optional"` // MFA 二次校验票据 value
}

// Validate 校验秘钥自检请求。
func (r *SecretKeySelfCheckReq) Validate() error {
	r.UUID = strings.TrimSpace(r.UUID)
	r.KeyVersion = strings.TrimSpace(r.KeyVersion)
	if r.UUID == "" {
		return errors.Errorf("秘钥标识不能为空")
	}
	return nil
}

// SecretKeyItem 表示秘钥管理响应项。
type SecretKeyItem struct {
	ID                     int                    `json:"id"`                     // 秘钥 ID
	UUID                   string                 `json:"uuid"`                   // AppID / API KEY 唯一标识
	Title                  string                 `json:"title"`                  // 秘钥标题
	KeyVersion             string                 `json:"keyVersion"`             // 当前详情或列表默认展示的版本号
	StableVersion          string                 `json:"stableVersion"`          // 稳定版本
	GrayVersion            string                 `json:"grayVersion"`            // 灰度版本
	GrayPercent            int                    `json:"grayPercent"`            // 灰度流量百分比
	AESKeyRef              string                 `json:"aesKeyRef"`              // AES KEY 文件绝对路径
	AESIVRef               string                 `json:"aesIvRef"`               // AES IV 文件绝对路径
	RSAPublicKeyUserRef    string                 `json:"rsaPublicKeyUserRef"`    // 用户 RSA 公钥文件绝对路径
	RSAPublicKeyServerRef  string                 `json:"rsaPublicKeyServerRef"`  // 服务端 RSA 公钥路径，可为空并由私钥派生
	RSAPrivateKeyServerRef string                 `json:"rsaPrivateKeyServerRef"` // 服务端 RSA 私钥文件绝对路径
	SecretMasked           bool                   `json:"secretMasked"`           // 当前响应中的敏感字段是否已脱敏
	Status                 int                    `json:"status"`                 // 应用状态：1 启用，0 禁用
	SignStatus             int                    `json:"signStatus"`             // 签名验签状态：1 启用，0 停用
	CryptoStatus           int                    `json:"cryptoStatus"`           // 加密解密状态：1 启用，0 停用
	VersionStatus          int                    `json:"versionStatus"`          // 当前版本状态：1 启用，0 停用
	VersionCount           int                    `json:"versionCount"`           // 当前应用下版本数量
	Remark                 string                 `json:"remark"`                 // 应用备注说明
	CreatedAt              string                 `json:"createdAt"`              // 创建时间
	UpdatedAt              string                 `json:"updatedAt"`              // 更新时间
	VersionList            []SecretKeyVersionItem `json:"versionList,omitempty"`  // 当前应用下的全部版本
}

// SecretKeyDetailReq 表示查询秘钥详情请求。
type SecretKeyDetailReq struct {
	ID           int    `path:"id" json:"id,optional" form:"id,optional"`           // 秘钥 ID
	KeyVersion   string `json:"keyVersion,optional" form:"keyVersion,optional"`     // 指定查询的版本号，默认稳定版本
	TwoStepKey   string `json:"twoStepKey,optional" form:"twoStepKey,optional"`     // 二次校验票据 key
	TwoStepValue string `json:"twoStepValue,optional" form:"twoStepValue,optional"` // 二次校验票据 value
}

// Validate 校验秘钥详情请求。
func (r *SecretKeyDetailReq) Validate() error {
	if r.ID <= 0 {
		return errors.Errorf("秘钥ID不能为空")
	}
	r.KeyVersion = strings.TrimSpace(r.KeyVersion)
	return nil
}

// CacheListReq 表示缓存管理列表请求。
type CacheListReq struct {
	Key string `json:"key,optional" form:"key,optional"` // 缓存键筛选
}

// Validate 校验缓存管理列表请求。
func (r *CacheListReq) Validate() error {
	r.Key = strings.TrimSpace(r.Key)
	return nil
}

// CacheKeyReq 表示缓存键查询请求。
type CacheKeyReq struct {
	Key        string `json:"key,optional" form:"key,optional"`       // Redis 缓存键
	Source     string `json:"source,optional" form:"source,optional"` // 请求来源：manual_search/route_jump/template_locate/template_drawer
	GetPageReq        // 复用分页参数
}

// Validate 校验缓存键查询请求。
func (r *CacheKeyReq) Validate() error {
	r.Key = strings.TrimSpace(r.Key)
	r.Source = strings.TrimSpace(r.Source)
	if r.Key == "" {
		return errors.Errorf("缓存key不能为空")
	}
	return nil
}

// CacheRenewReq 表示缓存刷新请求。
type CacheRenewReq struct {
	Key  string `json:"key,optional" form:"key,optional"`   // Redis 缓存键或缓存目标
	Type string `json:"type,optional" form:"type,optional"` // Redis 类型，兼容 laravel-admin 字段
}

// Validate 校验缓存刷新请求。
func (r *CacheRenewReq) Validate() error {
	r.Key = strings.TrimSpace(r.Key)
	r.Type = strings.TrimSpace(r.Type)
	if r.Key == "" {
		return errors.Errorf("缓存key不能为空")
	}
	return nil
}

// CacheWarmupReq 表示模板缓存预热请求。
// 预热用于“从无到有”批量构建缓存实例，解决模板 key 因 Redis 未命中而无法全量刷新的问题。
type CacheWarmupReq struct {
	TemplateKey string `json:"templateKey,optional" form:"templateKey,optional"` // 模板缓存键定义
	Limit       int    `json:"limit,optional" form:"limit,optional"`             // 限制本次最多预热多少个实例，避免一次性回源过大
}

// Validate 校验模板缓存预热请求。
func (r *CacheWarmupReq) Validate() error {
	r.TemplateKey = strings.TrimSpace(r.TemplateKey)
	if r.TemplateKey == "" {
		return errors.Errorf("模板缓存key不能为空")
	}
	if r.Limit < 0 {
		return errors.Errorf("预热数量限制不合法")
	}
	return nil
}

// CacheWarmupResp 表示模板缓存预热响应。
type CacheWarmupResp struct {
	TemplateKey string   `json:"templateKey"`          // 当前预热的模板缓存键定义
	Total       int      `json:"total"`                // 预热命中并处理的实例 key 总数
	Success     int      `json:"success"`              // 预热成功数量
	Failed      int      `json:"failed"`               // 预热失败数量
	FailedKeys  []string `json:"failedKeys,omitempty"` // 失败 key 采样列表（仅用于排查，避免返回过大）
	LatencyMS   int64    `json:"latencyMs"`            // 本次预热耗时（毫秒）
}

// CacheItem 表示可刷新的缓存目标。
type CacheItem struct {
	Index        string `json:"index"`        // 缓存目标索引
	Key          string `json:"key"`          // Redis 缓存键或模板
	KeyTitle     string `json:"keyTitle"`     // Redis 缓存键展示标题
	Type         string `json:"type"`         // Redis 数据类型
	Remark       string `json:"remark"`       // 缓存说明
	Category     string `json:"category"`     // 缓存分类：auth/permission/config/secret/session/system
	IsTemplate   bool   `json:"isTemplate"`   // 是否为模板型缓存 key
	ExampleKey   string `json:"exampleKey"`   // 模板型缓存示例 key，便于管理页直接复制查询
	AutoRebuild  bool   `json:"autoRebuild"`  // 查询详情 miss 时是否支持自动回源重建
	RefreshScope string `json:"refreshScope"` // 刷新粒度：single/all/prefix
}

// CacheKeyInfoResp 表示缓存键详情响应。
type CacheKeyInfoResp struct {
	Key   string     `json:"key"`            // Redis 缓存键
	Type  string     `json:"type"`           // Redis 数据类型
	TTL   int64      `json:"ttl"`            // Redis 剩余过期时间，单位秒
	Total int64      `json:"total"`          // 当前缓存值数量
	Value any        `json:"value"`          // 当前缓存值
	Item  *CacheItem `json:"item,omitempty"` // 当前 key 命中的缓存项元信息，便于前端展示说明与操作提示
}

// CacheSearchItem 表示缓存键搜索结果项。
type CacheSearchItem struct {
	Key  string     `json:"key"`            // 真实 Redis 缓存键
	Item *CacheItem `json:"item,omitempty"` // 当前缓存键命中的缓存项元信息，便于搜索结果直接展示分类与提示
}

// CacheSearchResp 表示缓存键搜索分页响应。
type CacheSearchResp struct {
	List           []CacheSearchItem `json:"list"`                     // 当前页真实 Redis Key 列表
	Total          int64             `json:"total"`                    // 当前搜索条件下已确认存在的 Redis Key 总数
	Page           int               `json:"page"`                     // 当前页码，从 1 开始
	PageSize       int               `json:"pageSize"`                 // 当前每页数量
	HasMore        bool              `json:"hasMore"`                  // 是否还有下一页，供前端流式加载或分页加载使用
	NextPage       int               `json:"nextPage,omitempty"`       // 下一页页码；没有下一页时为空
	SearchPath     string            `json:"searchPath,omitempty"`     // 本次搜索命中的后端链路：exact_exists/template_candidates
	ProviderName   string            `json:"providerName,omitempty"`   // 模板搜索 provider 名称，便于排查模板枚举来源
	TemplateKey    string            `json:"templateKey,omitempty"`    // 命中的模板 key 定义
	CandidateTotal int               `json:"candidateTotal,omitempty"` // 模板 provider 枚举出的候选 key 数量
	ExistingTotal  int               `json:"existingTotal,omitempty"`  // Redis 中已确认存在的 key 数量
	Limited        bool              `json:"limited"`                  // 是否触发后端最大搜索窗口保护
	MaxResults     int               `json:"maxResults"`               // 后端单次搜索最多确认的真实 key 数量
}

// UniquePositiveInts 对正整数列表去重并保持首次出现顺序。
func UniquePositiveInts(items []int) []int {
	result := make([]int, 0, len(items))
	for _, item := range items {
		if item <= 0 {
			continue
		}
		result = append(result, item)
	}
	return utils.Unique(result)
}

// RawJSONToString 把 JSON 原始值转成数据库 JSON 字段可接受的字符串。
func RawJSONToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "null"
	}
	return string(raw)
}

// normalizeRequestJSONValue 把请求里的任意 JSON 值统一转成原始 JSON 字节。
func normalizeRequestJSONValue(value any, allowEmpty bool) (json.RawMessage, error) {
	if value == nil {
		if allowEmpty {
			return nil, nil
		}
		return nil, errors.Errorf("配置值不能为空")
	}

	var raw json.RawMessage
	switch v := value.(type) {
	case json.RawMessage:
		raw = append(json.RawMessage(nil), v...)
	case []byte:
		raw = append(json.RawMessage(nil), v...)
	case string:
		text := strings.TrimSpace(v)
		if json.Valid([]byte(text)) {
			raw = json.RawMessage(text)
			break
		}
		body, err := json.Marshal(v)
		if err != nil {
			return nil, errors.Tag(err)
		}
		raw = body
	default:
		body, err := json.Marshal(v)
		if err != nil {
			return nil, errors.Tag(err)
		}
		raw = body
	}

	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		if allowEmpty {
			return nil, nil
		}
		return nil, errors.Errorf("配置值不能为空")
	}
	return raw, nil
}

// MustJSONNumber 把整数字符串格式化成 JSON 数字。
func MustJSONNumber(v int) json.RawMessage {
	return json.RawMessage(strconv.Itoa(v))
}
