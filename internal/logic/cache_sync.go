package logic

import "admin/internal/types"

// CacheSyncPendingResult 记录已提交操作的缓存同步错误，并返回可识别的待同步成功回执。
func CacheSyncPendingResult(
	logger interface{ Errorf(string, ...any) },
	successCode int,
	messageKey string,
	err error,
	format string,
	args ...any,
) *types.BizResult {
	LogWrappedError(logger, err, format, args...)
	return types.NewBizResult(successCode).
		SetI18nMessage(messageKey).
		WithData(&types.CacheSyncResp{SyncPending: true})
}
