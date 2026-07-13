package logic

import (
	stderrors "errors"
	"testing"

	"admin/common/codes"
	i18n "admin/common/i18n"
	"admin/internal/types"
)

// TestCacheSyncPendingResultKeepsCommittedOperationSuccessful 验证缓存同步失败不会诱导客户端重复提交已落库操作。
func TestCacheSyncPendingResultKeepsCommittedOperationSuccessful(t *testing.T) {
	result := CacheSyncPendingResult(nil, codes.UpdateSuccess, i18n.MsgKeyCacheSyncPending,
		stderrors.New("redis unavailable"), "更新后的缓存同步失败")
	if result == nil || !result.IsSuccess() {
		t.Fatalf("result = %+v, want committed operation success", result)
	}
	if result.MessageKey != i18n.MsgKeyCacheSyncPending {
		t.Fatalf("message key = %q, want %q", result.MessageKey, i18n.MsgKeyCacheSyncPending)
	}
	if result.Error != nil {
		t.Fatalf("committed operation must not expose retryable error: %v", result.Error)
	}
	data, ok := result.Data.(*types.CacheSyncResp)
	if !ok || !data.SyncPending {
		t.Fatalf("data = %#v, want syncPending=true", result.Data)
	}
}
