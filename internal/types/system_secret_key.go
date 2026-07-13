//lint:file-ignore SA5008 ignore go-zero optional tag

package types

import (
	"strings"

	"github.com/Is999/go-utils/errors"
)

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
