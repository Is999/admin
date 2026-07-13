package collectorx

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// TestIdempotencyTerminalCASRejectsExpiredOwner 验证旧 processing token 不能覆盖新的领取者。
func TestIdempotencyTerminalCASRejectsExpiredOwner(t *testing.T) {
	useCollectorTestAppID(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := newIdempotencyStore(client, time.Hour, time.Second, 20)
	event := Event{BizType: "biz", EventID: "event-1"}

	_, firstClaims, _, err := store.beginBatch(context.Background(), []Event{event})
	if err != nil || len(firstClaims) != 1 {
		t.Fatalf("first beginBatch() claims=%+v err=%v", firstClaims, err)
	}
	server.FastForward(2 * time.Second)
	_, secondClaims, _, err := store.beginBatch(context.Background(), []Event{event})
	if err != nil || len(secondClaims) != 1 {
		t.Fatalf("second beginBatch() claims=%+v err=%v", secondClaims, err)
	}
	if firstClaims[0].Token == secondClaims[0].Token {
		t.Fatal("重新领取必须生成不可复用 token")
	}
	if err = store.done(context.Background(), firstClaims); err == nil {
		t.Fatal("旧 token 写入终态应被 CAS 拒绝")
	}
	value, err := client.Get(context.Background(), secondClaims[0].Key).Result()
	if err != nil {
		t.Fatalf("读取新 claim 失败: %v", err)
	}
	if value != secondClaims[0].Token {
		t.Fatalf("旧 token 覆盖了新 claim value=%q want=%q", value, secondClaims[0].Token)
	}
	if err = store.done(context.Background(), secondClaims); err != nil {
		t.Fatalf("当前 token 写入终态失败: %v", err)
	}
	if value, _ = client.Get(context.Background(), secondClaims[0].Key).Result(); value != idempotencyDone {
		t.Fatalf("终态 value=%q want=%q", value, idempotencyDone)
	}
}

// TestIdempotencyRenewKeepsCurrentClaim 验证 processing token 续租后不会在原 TTL 到点被重复领取。
func TestIdempotencyRenewKeepsCurrentClaim(t *testing.T) {
	useCollectorTestAppID(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := newIdempotencyStore(client, time.Hour, time.Second, 20)
	event := Event{BizType: "biz", EventID: "event-1"}

	_, claims, _, err := store.beginBatch(context.Background(), []Event{event})
	if err != nil {
		t.Fatalf("beginBatch() error = %v", err)
	}
	server.FastForward(700 * time.Millisecond)
	if err = store.renew(context.Background(), claims); err != nil {
		t.Fatalf("renew() error = %v", err)
	}
	server.FastForward(700 * time.Millisecond)
	if _, _, _, err = store.beginBatch(context.Background(), []Event{event}); err == nil {
		t.Fatal("续租后的 processing token 应阻止其它 worker 提交 offset")
	}
	server.FastForward(400 * time.Millisecond)
	processEvents, nextClaims, duplicate, err := store.beginBatch(context.Background(), []Event{event})
	if err != nil || len(processEvents) != 1 || len(nextClaims) != 1 || duplicate != 0 {
		t.Fatalf("租约到期后应可重新领取 events=%+v claims=%+v duplicate=%d err=%v", processEvents, nextClaims, duplicate, err)
	}
}

// TestIdempotencyBeginDistinguishesStoredStates 验证只有完成和失败终态可安全跳过。
func TestIdempotencyBeginDistinguishesStoredStates(t *testing.T) {
	tests := []struct {
		name          string // 测试场景名称
		state         string // 预置的 Redis 幂等状态
		wantErr       bool   // 是否期望占用失败
		wantDuplicate int    // 期望识别的终态重复数量
	}{
		{name: "done", state: idempotencyDone, wantDuplicate: 1},
		{name: "failed", state: idempotencyFailed, wantDuplicate: 1},
		{name: "processing", state: idempotencyProcessingPrefix + "other", wantErr: true},
		{name: "invalid", state: "unknown", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useCollectorTestAppID(t)
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { _ = client.Close() })
			store := newIdempotencyStore(client, time.Hour, time.Minute, 20)
			event := Event{BizType: "biz", EventID: "event-1"}
			claim, err := store.newClaim(event)
			if err != nil {
				t.Fatalf("newClaim() error = %v", err)
			}
			if err = client.Set(context.Background(), claim.Key, tt.state, time.Hour).Err(); err != nil {
				t.Fatalf("写入预置状态失败: %v", err)
			}

			events, claims, duplicate, err := store.beginBatch(context.Background(), []Event{event})
			if (err != nil) != tt.wantErr {
				t.Fatalf("beginBatch() err=%v wantErr=%v", err, tt.wantErr)
			}
			if len(events) != 0 || len(claims) != 0 || duplicate != tt.wantDuplicate {
				t.Fatalf("beginBatch() events=%+v claims=%+v duplicate=%d", events, claims, duplicate)
			}
			if value, getErr := client.Get(context.Background(), claim.Key).Result(); getErr != nil || value != tt.state {
				t.Fatalf("预置状态被意外修改 value=%q err=%v", value, getErr)
			}
		})
	}
}

// TestIdempotencyBeginDeduplicatesWithinBatch 验证批内重复事件只生成一个占用 token。
func TestIdempotencyBeginDeduplicatesWithinBatch(t *testing.T) {
	useCollectorTestAppID(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := newIdempotencyStore(client, time.Hour, time.Minute, 20)
	event := Event{BizType: " biz ", EventID: " event-1 "}

	events, claims, duplicate, err := store.beginBatch(context.Background(), []Event{event, event})
	if err != nil || len(events) != 1 || len(claims) != 1 || duplicate != 1 {
		t.Fatalf("beginBatch() events=%+v claims=%+v duplicate=%d err=%v", events, claims, duplicate, err)
	}
}

// TestIdempotencyBeginRollsBackClaimsOnBusy 验证同分块或后续分块遇到占用时会回滚当前批次已领取 token。
func TestIdempotencyBeginRollsBackClaimsOnBusy(t *testing.T) {
	for _, pipelineSize := range []int{20, 1} {
		t.Run("pipeline_size_"+strconv.Itoa(pipelineSize), func(t *testing.T) {
			useCollectorTestAppID(t)
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { _ = client.Close() })
			store := newIdempotencyStore(client, time.Hour, time.Minute, pipelineSize)
			events := []Event{{BizType: "biz", EventID: "event-1"}, {BizType: "biz", EventID: "event-2"}}
			first, err := store.newClaim(events[0])
			if err != nil {
				t.Fatalf("first newClaim() error = %v", err)
			}
			second, err := store.newClaim(events[1])
			if err != nil {
				t.Fatalf("second newClaim() error = %v", err)
			}
			busyState := idempotencyProcessingPrefix + "other"
			if err = client.Set(context.Background(), second.Key, busyState, time.Hour).Err(); err != nil {
				t.Fatalf("写入其它 worker 占用失败: %v", err)
			}

			if _, _, _, err = store.beginBatch(context.Background(), events); err == nil {
				t.Fatal("批次遇到其它 worker 占用应返回错误")
			}
			if exists, existsErr := client.Exists(context.Background(), first.Key).Result(); existsErr != nil || exists != 0 {
				t.Fatalf("已领取 token 未回滚 exists=%d err=%v", exists, existsErr)
			}
			if value, getErr := client.Get(context.Background(), second.Key).Result(); getErr != nil || value != busyState {
				t.Fatalf("其它 worker 占用被意外修改 value=%q err=%v", value, getErr)
			}
		})
	}
}
