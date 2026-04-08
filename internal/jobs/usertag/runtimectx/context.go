package runtimectx

import (
	"context"
	"fmt"

	"admin_cron/internal/infra/loggerx"
	"admin_cron/internal/jobs/usertag/types"
	"admin_cron/internal/svc"

	"github.com/Is999/go-utils/errors"
	"github.com/zeromicro/go-zero/core/logx"
)

// Context 是 usertag 单个节点执行期上下文。
// 它集中保存链路字段和外部依赖，避免日志字段在各层重复拼装。
type Context struct {
	Context    context.Context      // Go 标准上下文，用于取消、超时和日志链路
	Service    *svc.ServiceContext  // 全局依赖上下文，包含 MySQL、Redis、Kafka
	Options    types.RuntimeOptions // 当前工作流解析后的运行参数
	Node       string               // 当前节点名称
	Shard      int                  // 当前分片下标
	ShardTotal int                  // 当前分片总数
	Batch      int                  // 当前批次序号，未进入分批时为 0
}

// New 创建 usertag 节点上下文。
func New(ctx context.Context, svcCtx *svc.ServiceContext, opts types.RuntimeOptions, node string) *Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return &Context{
		Context:    ctx,
		Service:    svcCtx,
		Options:    opts,
		Node:       node,
		Shard:      opts.ShardIndex,
		ShardTotal: opts.ShardTotal,
	}
}

// WithNode 返回切换节点名后的上下文副本。
func (c *Context) WithNode(node string) *Context {
	next := c.clone()
	next.Node = node
	return next
}

// WithShard 返回切换分片后的上下文副本。
func (c *Context) WithShard(shard, shardTotal int) *Context {
	next := c.clone()
	next.Shard = shard
	next.ShardTotal = shardTotal
	return next
}

// WithBatch 返回切换批次后的上下文副本。
func (c *Context) WithBatch(batch int) *Context {
	next := c.clone()
	next.Batch = batch
	return next
}

// Infof 统一输出 usertag 关键路径信息日志。
func (c *Context) Infof(format string, args ...any) {
	loggerx.Infow(c.baseContext(), fmt.Sprintf(c.prefix()+format, args...), c.logFields()...)
}

// Errorf 统一输出 usertag 关键路径错误日志。
func (c *Context) Errorf(format string, args ...any) {
	message := fmt.Sprintf(c.prefix()+format, args...)
	loggerx.ErrorTextw(c.baseContext(), message, message, c.logFields()...)
}

// Wrap 为跨层错误追加 usertag 链路信息。
func (c *Context) Wrap(err error, action string) error {
	if err == nil {
		return nil
	}
	return errors.Wrapf(err, "用户标签%s workflow_id=%s mode=%s node=%s shard=%d/%d batch=%d",
		action, c.WorkflowID(), c.Mode(), c.Node, c.Shard, c.ShardTotal, c.Batch)
}

// WorkflowID 返回当前工作流 ID。
func (c *Context) WorkflowID() string {
	if c == nil {
		return ""
	}
	return c.Options.WorkflowID
}

// Mode 返回当前运行模式。
func (c *Context) Mode() string {
	if c == nil {
		return ""
	}
	return c.Options.Mode
}

// TraceFields 返回统一链路字段文本，供仓储或测试复用。
func (c *Context) TraceFields() string {
	return fmt.Sprintf("workflow_id=%s mode=%s node=%s shard=%d/%d batch=%d",
		c.WorkflowID(), c.Mode(), c.Node, c.Shard, c.ShardTotal, c.Batch)
}

// logFields 返回用户标签执行期结构化日志字段，保证 workflow_id / mode / node / shard 可直接检索。
func (c *Context) logFields() []logx.LogField {
	if c == nil {
		return nil
	}
	return []logx.LogField{
		logx.Field("workflow_id", c.WorkflowID()), // 当前工作流实例 ID
		logx.Field("mode", c.Mode()),              // 当前标签计算模式
		logx.Field("node", c.Node),                // 当前执行节点
		logx.Field("shard_index", c.Shard),        // 当前分片下标
		logx.Field("shard_total", c.ShardTotal),   // 当前分片总数
		logx.Field("batch", c.Batch),              // 当前批次序号
	}
}

// clone 复制上下文值，避免不同阶段修改同一个指针导致日志串线。
func (c *Context) clone() *Context {
	if c == nil {
		return New(context.Background(), nil, types.RuntimeOptions{}, "")
	}
	return new(*c)
}

// baseContext 返回日志使用的标准上下文。
func (c *Context) baseContext() context.Context {
	if c == nil || c.Context == nil {
		return context.Background()
	}
	return c.Context
}

// prefix 生成统一日志前缀，高频内层循环不应直接调用日志方法。
func (c *Context) prefix() string {
	return "用户标签 " + c.TraceFields() + " "
}
