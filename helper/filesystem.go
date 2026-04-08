package helper

import (
	stdErrors "errors"
	"os"
	"syscall"
)

// IsCrossDeviceRenameError 判断 rename 失败是否由跨设备链接 EXDEV 引起。
// 本地存储和分片上传在跨挂载点提交临时文件时会触发该错误，调用方据此降级为流式复制。
func IsCrossDeviceRenameError(err error) bool {
	if err == nil {
		return false
	}
	if stdErrors.Is(err, syscall.EXDEV) {
		return true
	}
	var linkErr *os.LinkError
	if stdErrors.As(err, &linkErr) {
		return stdErrors.Is(linkErr.Err, syscall.EXDEV)
	}
	return false
}
