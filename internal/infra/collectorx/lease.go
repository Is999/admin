package collectorx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/Is999/go-utils/errors"
)

// leaseHeartbeat 在 Processor 执行期间续租共享状态，失去所有权时立即取消业务上下文。
type leaseHeartbeat struct {
	parent   context.Context             // Processor 的父上下文
	stop     func()                      // 停止后台续租
	done     <-chan error                // 后台续租退出结果
	renew    func(context.Context) error // 执行所有权 CAS 续租
	interval time.Duration               // 租约心跳间隔，也用于限制单次最终续租耗时
}

// startLeaseHeartbeat 启动租约续期；renew 必须执行所有权 CAS。
func startLeaseHeartbeat(parent context.Context, interval time.Duration, cancelProcessor context.CancelFunc, renew func(context.Context) error) *leaseHeartbeat {
	if parent == nil {
		parent = context.Background()
	}
	if interval <= 0 {
		interval = time.Second
	}
	stopCh := make(chan struct{})
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() { close(stopCh) })
	}
	done := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				done <- nil
				return
			case <-parent.Done():
				done <- errors.Tag(parent.Err())
				return
			case <-ticker.C:
				renewCtx, cancelRenew := context.WithTimeout(parent, leaseRenewTimeout(interval))
				err := renew(renewCtx)
				cancelRenew()
				if err != nil {
					if cancelProcessor != nil {
						cancelProcessor()
					}
					done <- errors.Tag(err)
					return
				}
			}
		}
	}()
	return &leaseHeartbeat{parent: parent, stop: stop, done: done, renew: renew, interval: interval}
}

// StopAndRenew 停止后台续租并执行一次最终续租，为后续状态 CAS 留出完整租约窗口。
func (h *leaseHeartbeat) StopAndRenew() error {
	if h == nil {
		return nil
	}
	h.stop()
	if err := <-h.done; err != nil {
		return errors.Tag(err)
	}
	if err := h.parent.Err(); err != nil {
		return errors.Tag(err)
	}
	ctx, cancel := context.WithTimeout(h.parent, leaseRenewTimeout(h.interval))
	defer cancel()
	return errors.Tag(h.renew(ctx))
}

// leaseRenewTimeout 限制单次续租调用耗时，正常停止不会取消正在执行的续租。
func leaseRenewTimeout(interval time.Duration) time.Duration {
	timeout := interval
	if timeout > 5*time.Second {
		return 5 * time.Second
	}
	if timeout < 100*time.Millisecond {
		return 100 * time.Millisecond
	}
	return timeout
}

// leaseRenewInterval 返回租约三分之一的续期间隔，并限制极端配置下的轮询频率。
func leaseRenewInterval(lease time.Duration) time.Duration {
	interval := lease / 3
	if interval < 100*time.Millisecond {
		return 100 * time.Millisecond
	}
	if interval > 30*time.Second {
		return 30 * time.Second
	}
	return interval
}

// newFailureClaimToken 生成失败账本不可复用的随机领取令牌。
func newFailureClaimToken() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", errors.Tag(err)
	}
	return hex.EncodeToString(raw), nil
}
