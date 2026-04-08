package taskruntime

import "admin_cron/internal/types"

// resultError 把统一业务响应中的错误对象提取为标准 error，便于任务执行链直接返回。
func resultError(resp *types.BizResult) error {
	if resp == nil {
		return nil
	}
	return resp.Error
}
