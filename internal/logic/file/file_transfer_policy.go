package file

import (
	"strings"
	"sync"

	"admin/pkg/storage"

	"github.com/Is999/go-utils/errors"
)

// fileUploadPolicyRegistry 保存文件上传业务策略注册表。
var fileUploadPolicyRegistry = struct {
	mu       sync.RWMutex                // 保护 policies
	policies map[string]FileUploadPolicy // bizType -> 上传策略
}{
	policies: map[string]FileUploadPolicy{
		FileTransferBizAdminAvatar: {
			MaxSize:            5 * 1024 * 1024,                                    // 管理员头像最大 5MB
			AllowedExts:        []string{".jpg", ".jpeg", ".png", ".gif", ".webp"}, // 头像允许图片扩展名
			Visibility:         storage.VisibilityPublic,                           // 头像允许公开访问
			AllowPublicAccess:  true,                                               // 头像下载接口允许匿名读取
			AllowDirectUpload:  true,                                               // S3 场景允许前端直传
			ExpectedMIMEPrefix: "image/",                                           // 完成后校验文件内容 MIME
		},
		FileTransferBizSysConfigExcelImport: {
			MaxSize:           20 * 1024 * 1024,          // 系统配置导入文件最大 20MB
			AllowedExts:       []string{".xlsx"},         // 系统配置导入仅允许 xlsx
			Visibility:        storage.VisibilityPrivate, // 导入文件必须走受控下载
			AllowDirectUpload: true,                      // S3 场景允许前端直传
		},
	},
}

// RegisterFileUploadPolicy 注册文件上传业务策略，便于后续业务模块扩展上传类型。
func RegisterFileUploadPolicy(bizType string, policy FileUploadPolicy) error {
	bizType = strings.TrimSpace(bizType)
	if bizType == "" {
		return errors.Errorf("上传业务类型不能为空")
	}
	if err := validateFileUploadPolicy(policy); err != nil {
		return errors.Tag(err)
	}

	fileUploadPolicyRegistry.mu.Lock()
	defer fileUploadPolicyRegistry.mu.Unlock()
	if _, exists := fileUploadPolicyRegistry.policies[bizType]; exists {
		return errors.Errorf("上传业务类型已注册: %s", bizType)
	}
	fileUploadPolicyRegistry.policies[bizType] = policy
	return nil
}

// fileUploadPolicyOf 读取指定上传业务类型的策略。
func fileUploadPolicyOf(bizType string) (FileUploadPolicy, error) {
	bizType = strings.TrimSpace(bizType)
	if bizType == "" {
		return FileUploadPolicy{}, errors.Errorf("上传业务类型不能为空")
	}
	fileUploadPolicyRegistry.mu.RLock()
	policy, exists := fileUploadPolicyRegistry.policies[bizType]
	fileUploadPolicyRegistry.mu.RUnlock()
	if !exists {
		return FileUploadPolicy{}, errors.Errorf("不支持的上传业务类型")
	}
	return policy, nil
}

// validateFileUploadPolicy 校验上传策略基础约束，避免注册危险策略。
func validateFileUploadPolicy(policy FileUploadPolicy) error {
	if policy.MaxSize <= 0 {
		return errors.Errorf("上传文件大小限制必须大于 0")
	}
	if len(policy.AllowedExts) == 0 {
		return errors.Errorf("上传文件扩展名白名单不能为空")
	}
	for _, ext := range policy.AllowedExts {
		ext = strings.TrimSpace(ext)
		if ext == "" || !strings.HasPrefix(ext, ".") || strings.ContainsAny(ext, `/\`) || strings.Contains(ext, "..") {
			return errors.Errorf("上传文件扩展名不合法")
		}
	}
	if strings.TrimSpace(policy.Visibility) == "" {
		return errors.Errorf("上传文件可见性不能为空")
	}
	return nil
}
