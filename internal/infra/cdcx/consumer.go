package cdcx

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"sort"
	"strings"
	"sync"
	"time"

	"admin/internal/config"
	"admin/internal/infra/loggerx"

	"github.com/Is999/go-utils/errors"
	"github.com/segmentio/kafka-go"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	defaultCDCReadTimeout   = 10 * time.Second // 默认单次读取超时时间
	defaultCDCRetryBackoff  = time.Second      // 默认失败重试间隔
	defaultCDCMaxRetryTimes = 8                // 默认单条消息最大失败次数
	defaultCDCConsumer      = "admin-cdc"      // 默认消费者名前缀
)

// Processor 处理单条 CDC 事件。
type Processor interface {
	ProcessCDC(context.Context, Event) error
}

// ProcessorFunc 允许直接用函数注册 CDC 处理器。
type ProcessorFunc func(context.Context, Event) error

// ProcessCDC 执行函数式 CDC 处理器。
func (fn ProcessorFunc) ProcessCDC(ctx context.Context, event Event) error {
	if fn == nil {
		return errors.Errorf("CDC processor 为空")
	}
	return fn(ctx, event)
}

// Consumer 负责订阅 Debezium Topic 并分发表级处理器。
type Consumer struct {
	cfg           config.CDCConfig     // CDC 消费配置
	processors    map[string]Processor // db.table 到处理器的映射
	metadataProbe topicMetadataProbe   // Kafka Topic 元数据只读探针

	mu         sync.RWMutex          // 保护 processors、attempts 和状态快照
	attempts   map[string]int        // 位点失败次数
	status     ConsumerStatus        // 消费运行状态快照
	topicStats map[string]TopicStats // Topic 维度状态
	ctx        context.Context       // 后台生命周期上下文
	cancel     context.CancelFunc    // 后台停止函数
	wg         sync.WaitGroup        // 等待所有 Topic 消费协程退出
	readers    []*kafka.Reader       // Kafka reader 列表
	deadWriter messageWriter         // 死信写入器
}

// messageWriter 抽象 Kafka writer，便于测试死信路径。
type messageWriter interface {
	WriteMessages(context.Context, ...kafka.Message) error
	Close() error
}

// topicMetadataProbe 只读校验 Kafka Topic 元数据，不在 readiness 中写测试消息。
type topicMetadataProbe interface {
	Check(context.Context, []string, string) error
}

// kafkaTopicMetadataProbe 使用 Kafka 元数据接口校验 Topic 是否存在且包含分区。
type kafkaTopicMetadataProbe struct {
	dialer *kafka.Dialer // Kafka 元数据连接器
}

// Check 依次尝试可用 broker，任一返回目标 Topic 分区即视为成功。
func (p kafkaTopicMetadataProbe) Check(ctx context.Context, brokers []string, topic string) error {
	if p.dialer == nil {
		return errors.New("CDC Kafka 元数据探针未初始化")
	}
	var lastErr error
	for _, broker := range brokers {
		partitions, err := p.dialer.LookupPartitions(ctx, "tcp", broker, topic)
		if err == nil && len(partitions) > 0 {
			return nil
		}
		if err == nil {
			err = errors.Errorf("CDC Topic不存在或无分区 topic=%s", topic)
		}
		lastErr = err
		if ctx.Err() != nil {
			break
		}
	}
	if lastErr == nil {
		lastErr = errors.Errorf("CDC Kafka broker为空 topic=%s", topic)
	}
	return errors.Tag(lastErr)
}

// New 创建 CDC 消费器。
func New(cfg config.CDCConfig) (*Consumer, error) {
	cfg.GroupID = strings.TrimSpace(cfg.GroupID)
	cfg.ConsumerName = strings.TrimSpace(cfg.ConsumerName)
	if cfg.ConsumerName == "" {
		cfg.ConsumerName = defaultCDCConsumer
	}
	if cfg.ReadTimeoutSeconds <= 0 {
		cfg.ReadTimeoutSeconds = int(defaultCDCReadTimeout / time.Second)
	}
	if cfg.RetryBackoffSeconds <= 0 {
		cfg.RetryBackoffSeconds = int(defaultCDCRetryBackoff / time.Second)
	}
	if cfg.MaxRetryTimes <= 0 {
		cfg.MaxRetryTimes = defaultCDCMaxRetryTimes
	}
	cfg.DeadLetterTopic = strings.TrimSpace(cfg.DeadLetterTopic)
	if cfg.Enabled {
		if len(normalizeStrings(cfg.Brokers)) == 0 {
			return nil, errors.Errorf("cdc.enabled=true 时必须配置 cdc.brokers 或 kafka.brokers")
		}
		if cfg.GroupID == "" {
			return nil, errors.Errorf("cdc.enabled=true 时必须配置 cdc.group_id")
		}
		if len(enabledTopics(cfg.Topics)) == 0 {
			return nil, errors.Errorf("cdc.enabled=true 时必须配置 cdc.topics")
		}
	}
	cfg.Brokers = normalizeStrings(cfg.Brokers)
	cfg.Topics = normalizeTopics(cfg.Topics)
	return &Consumer{
		cfg:           cfg,
		processors:    make(map[string]Processor),
		metadataProbe: kafkaTopicMetadataProbe{dialer: &kafka.Dialer{Timeout: time.Second}},
		attempts:      make(map[string]int),
		topicStats:    make(map[string]TopicStats),
	}, nil
}

// RegisterProcessor 注册表级 CDC 处理器。
func (c *Consumer) RegisterProcessor(table string, processor Processor) error {
	if c == nil {
		return errors.Errorf("CDC consumer 未初始化")
	}
	table = normalizeTable(table)
	if table == "" {
		return errors.Errorf("CDC processor 表名为空")
	}
	if processor == nil {
		return errors.Errorf("CDC processor 为空 table=%s", table)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.processors[table]; ok {
		return errors.Errorf("CDC processor 重复注册 table=%s", table)
	}
	c.processors[table] = processor
	return nil
}

// ValidateProcessors 校验启用 Topic 已注册处理器。
func (c *Consumer) ValidateProcessors() error {
	if c == nil || !c.cfg.Enabled {
		return nil
	}
	for _, topic := range c.cfg.Topics {
		if !topic.Enabled {
			continue
		}
		table := normalizeTable(topic.Table)
		if table == "" {
			continue
		}
		if c.processor(table) == nil {
			return errors.Errorf("CDC Topic 未注册处理器 table=%s topic=%s", table, topic.Topic)
		}
	}
	return nil
}

// RegisteredTables 返回已注册处理器表路由键。
func (c *Consumer) RegisteredTables() []string {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	tables := make([]string, 0, len(c.processors))
	for table := range c.processors {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	return tables
}

// Ready 检查 CDC 处理器、后台运行态和已启用 Kafka Topic 的元数据连通性。
func (c *Consumer) Ready(ctx context.Context, requireWorker bool) error {
	if c == nil {
		return errors.New("CDC consumer 未初始化")
	}
	if !c.cfg.Enabled {
		return nil
	}
	if err := c.ValidateProcessors(); err != nil {
		return errors.Tag(err)
	}
	if requireWorker && !c.Snapshot().Running {
		return errors.New("CDC consumer 未运行")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if c.metadataProbe == nil {
		return errors.New("CDC Kafka 元数据探针未初始化")
	}
	for _, topic := range c.readinessTopics() {
		if err := c.metadataProbe.Check(ctx, c.cfg.Brokers, topic); err != nil {
			return errors.Wrapf(err, "CDC Kafka Topic不可用 topic=%s", topic)
		}
	}
	return nil
}

// readinessTopics 返回所有启用源 Topic 和死信 Topic，去重后稳定排序。
func (c *Consumer) readinessTopics() []string {
	if c == nil {
		return nil
	}
	unique := make(map[string]struct{}, len(c.cfg.Topics)+1)
	for _, topic := range c.cfg.Topics {
		if topic.Enabled && strings.TrimSpace(topic.Topic) != "" {
			unique[strings.TrimSpace(topic.Topic)] = struct{}{}
		}
	}
	if topic := strings.TrimSpace(c.cfg.DeadLetterTopic); topic != "" {
		unique[topic] = struct{}{}
	}
	topics := make([]string, 0, len(unique))
	for topic := range unique {
		topics = append(topics, topic)
	}
	sort.Strings(topics)
	return topics
}

// RegisterProcessorFunc 注册函数式表级 CDC 处理器。
func (c *Consumer) RegisterProcessorFunc(table string, fn ProcessorFunc) error {
	if fn == nil {
		return errors.Errorf("CDC processor 函数为空")
	}
	return errors.Tag(c.RegisterProcessor(table, fn))
}

// Start 启动 CDC 消费协程。
func (c *Consumer) Start() {
	if c == nil || !c.cfg.Enabled || c.cancel != nil {
		return
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.markStarted()
	if c.cfg.DeadLetterTopic != "" {
		c.deadWriter = &kafka.Writer{
			Addr:                   kafka.TCP(c.cfg.Brokers...),
			Topic:                  c.cfg.DeadLetterTopic,
			AllowAutoTopicCreation: true,
		}
	}
	for _, topic := range c.cfg.Topics {
		if !topic.Enabled {
			continue
		}
		reader := kafka.NewReader(kafka.ReaderConfig{
			Brokers:     c.cfg.Brokers,
			GroupID:     c.cfg.GroupID,
			Topic:       strings.TrimSpace(topic.Topic),
			StartOffset: kafka.FirstOffset,
		})
		c.readers = append(c.readers, reader)
		c.wg.Add(1)
		go func(rule config.CDCTopicConfig, r *kafka.Reader) {
			defer c.wg.Done()
			c.runTopic(rule, r)
		}(topic, reader)
	}
}

// Stop 停止 CDC 消费协程并关闭 Kafka reader。
func (c *Consumer) Stop(ctx context.Context) error {
	if c == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if c.cancel != nil {
		c.cancel()
	}
	var firstErr error
	recordErr := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = errors.Tag(err)
		}
	}
	readerClosers := make([]func() error, 0, len(c.readers))
	for _, reader := range c.readers {
		readerClosers = append(readerClosers, reader.Close)
	}
	recordErr(closeAllWithContext(ctx, readerClosers))
	if ctx.Err() != nil {
		return errors.Tag(firstErr)
	}
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		recordErr(errors.Wrap(ctx.Err(), "等待 CDC 消费协程退出超时"))
		return errors.Tag(firstErr)
	}
	if c.deadWriter != nil {
		recordErr(closeAllWithContext(ctx, []func() error{c.deadWriter.Close}))
	}
	c.markStopped()
	return errors.Tag(firstErr)
}

// closeAllWithContext 并发关闭彼此独立的 Kafka 资源，并受应用停止期限约束。
func closeAllWithContext(ctx context.Context, closeFns []func() error) error {
	if len(closeFns) == 0 {
		return nil
	}
	errs := make(chan error, len(closeFns))
	var wg sync.WaitGroup
	wg.Add(len(closeFns))
	for _, closeFn := range closeFns {
		go func() {
			defer wg.Done()
			errs <- closeFn()
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		close(errs)
		for err := range errs {
			if err != nil {
				return errors.Tag(err)
			}
		}
		return nil
	case <-ctx.Done():
		return errors.Tag(ctx.Err())
	}
}

// runTopic 持续消费单个 Topic，处理成功后才提交 offset。
func (c *Consumer) runTopic(rule config.CDCTopicConfig, reader *kafka.Reader) {
	readTimeout := time.Duration(c.cfg.ReadTimeoutSeconds) * time.Second
	if readTimeout <= 0 {
		readTimeout = defaultCDCReadTimeout
	}
	for {
		if c.ctx == nil {
			return
		}
		readCtx, cancel := context.WithTimeout(c.ctx, readTimeout)
		msg, err := reader.FetchMessage(readCtx)
		cancel()
		if err != nil {
			if stderrors.Is(err, context.Canceled) || stderrors.Is(err, context.DeadlineExceeded) {
				if c.ctx.Err() != nil {
					return
				}
				continue
			}
			loggerx.Errorw(c.ctx, "CDC 读取 Kafka 消息失败", err,
				logx.Field("topic", rule.Topic),
			)
			time.Sleep(time.Second)
			continue
		}

		c.markFetched(msg)
		if !c.processFetchedMessage(rule, msg) {
			return
		}
		if !c.commitFetchedMessage(msg, reader.CommitMessages) {
			return
		}
	}
}

// commitFetchedMessage 原位重试当前消息位点，禁止后续高位点掩盖提交失败。
func (c *Consumer) commitFetchedMessage(msg kafka.Message, commit func(context.Context, ...kafka.Message) error) bool {
	for {
		if c.ctx == nil || c.ctx.Err() != nil {
			return false
		}
		if err := commit(c.ctx, msg); err == nil {
			return true
		} else {
			loggerx.Errorw(c.ctx, "CDC 提交 Kafka offset 失败", err,
				logx.Field("topic", msg.Topic),
				logx.Field("partition", msg.Partition),
				logx.Field("offset", msg.Offset),
			)
		}
		c.sleepAfterFailure()
	}
}

// processFetchedMessage 处理同一条消息直到成功或死信，避免提交后续 offset 跳过失败消息。
func (c *Consumer) processFetchedMessage(rule config.CDCTopicConfig, msg kafka.Message) bool {
	for {
		if c.ctx == nil || c.ctx.Err() != nil {
			return false
		}
		err := c.handleMessage(c.ctx, rule, msg)
		if err == nil {
			c.clearFailure(msg)
			c.markProcessed(msg)
			return true
		}
		if IsSkip(err) {
			c.clearFailure(msg)
			c.markSkipped(msg, err)
			loggerx.Infow(c.ctx, "CDC 消息已跳过",
				logx.Field("topic", msg.Topic),
				logx.Field("partition", msg.Partition),
				logx.Field("offset", msg.Offset),
				logx.Field("reason", err.Error()),
			)
			return true
		}
		attempt := c.recordFailure(msg)
		c.markFailed(msg, err)
		if c.shouldDeadLetter(attempt) {
			if deadErr := c.writeDeadLetter(c.ctx, rule, msg, err, attempt); deadErr != nil {
				loggerx.Errorw(c.ctx, "CDC 写入死信失败", deadErr,
					logx.Field("topic", msg.Topic),
					logx.Field("partition", msg.Partition),
					logx.Field("offset", msg.Offset),
					logx.Field("attempt", attempt),
				)
				c.sleepAfterFailure()
				continue
			}
			loggerx.Errorw(c.ctx, "CDC 消息进入死信", err,
				logx.Field("topic", msg.Topic),
				logx.Field("partition", msg.Partition),
				logx.Field("offset", msg.Offset),
				logx.Field("attempt", attempt),
				logx.Field("dead_letter_topic", c.cfg.DeadLetterTopic),
			)
			c.clearFailure(msg)
			c.markDeadLettered(msg, err)
			return true
		}
		loggerx.Errorw(c.ctx, "CDC 处理 Kafka 消息失败", err,
			logx.Field("topic", msg.Topic),
			logx.Field("partition", msg.Partition),
			logx.Field("offset", msg.Offset),
			logx.Field("attempt", attempt),
		)
		c.sleepAfterFailure()
	}
}

// handleMessage 解析并分发单条 Kafka 消息。
func (c *Consumer) handleMessage(ctx context.Context, rule config.CDCTopicConfig, msg kafka.Message) error {
	event, err := DecodeDebeziumEvent(msg.Topic, msg.Partition, msg.Offset, msg.Key, msg.Value)
	if err != nil {
		return errors.Tag(err)
	}
	table := normalizeTable(rule.Table)
	if table == "" {
		table = normalizeTable(event.TableKey())
	}
	if table == "" {
		return errors.Errorf("CDC 事件缺少表路由 topic=%s partition=%d offset=%d", msg.Topic, msg.Partition, msg.Offset)
	}
	processor := c.processor(table)
	if processor == nil {
		return errors.Errorf("CDC 未注册表处理器 table=%s topic=%s", table, msg.Topic)
	}
	return errors.Tag(processor.ProcessCDC(ctx, event))
}

// Snapshot 返回当前 CDC 消费器状态快照。
func (c *Consumer) Snapshot() ConsumerStatus {
	if c == nil {
		return ConsumerStatus{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := c.status
	out.Topics = make(map[string]TopicStats, len(c.topicStats))
	for topic, stat := range c.topicStats {
		out.Topics[topic] = stat
	}
	out.RegisteredTables = make([]string, 0, len(c.processors))
	for table := range c.processors {
		out.RegisteredTables = append(out.RegisteredTables, table)
	}
	sort.Strings(out.RegisteredTables)
	return out
}

// writeDeadLetter 写入包含原消息和失败原因的死信消息。
func (c *Consumer) writeDeadLetter(ctx context.Context, rule config.CDCTopicConfig, msg kafka.Message, cause error, attempt int) error {
	if c.deadWriter == nil || c.cfg.DeadLetterTopic == "" {
		return errors.Errorf("CDC 未配置死信 topic")
	}
	body, err := json.Marshal(DeadLetterMessage{
		SourceTopic:     msg.Topic,
		SourcePartition: msg.Partition,
		SourceOffset:    msg.Offset,
		Table:           normalizeTable(rule.Table),
		Attempt:         attempt,
		Error:           strings.TrimSpace(cause.Error()),
		Key:             cloneBytes(msg.Key),
		Value:           cloneBytes(msg.Value),
		CreatedAt:       time.Now(),
	})
	if err != nil {
		return errors.Wrap(err, "编码 CDC 死信消息失败")
	}
	return errors.Tag(c.deadWriter.WriteMessages(ctx, kafka.Message{
		Key:   []byte(messageKey(msg)),
		Value: body,
	}))
}

// shouldDeadLetter 判断当前失败次数是否达到死信阈值。
func (c *Consumer) shouldDeadLetter(attempt int) bool {
	return c.cfg.DeadLetterTopic != "" && c.cfg.MaxRetryTimes > 0 && attempt >= c.cfg.MaxRetryTimes
}

// recordFailure 记录单条消息失败次数。
func (c *Consumer) recordFailure(msg kafka.Message) int {
	key := messageKey(msg)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.attempts[key]++
	return c.attempts[key]
}

// clearFailure 清理已处理或已死信消息的失败计数。
func (c *Consumer) clearFailure(msg kafka.Message) {
	key := messageKey(msg)
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.attempts, key)
}

// markStarted 记录消费者启动状态。
func (c *Consumer) markStarted() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status.Enabled = c.cfg.Enabled
	c.status.Running = true
	c.status.GroupID = c.cfg.GroupID
	c.status.ConsumerName = c.cfg.ConsumerName
	c.status.StartedAt = time.Now()
	c.status.StoppedAt = time.Time{}
}

// markStopped 记录消费者停止状态。
func (c *Consumer) markStopped() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status.Running = false
	c.status.StoppedAt = time.Now()
}

// markFetched 记录已拉取消息。
func (c *Consumer) markFetched(msg kafka.Message) {
	c.updateTopicStats(msg, nil, func(stat *TopicStats) {
		stat.Fetched++
		stat.LastFetchedAt = time.Now()
		stat.LastPartition = msg.Partition
		stat.LastOffset = msg.Offset
	})
}

// markProcessed 记录已处理消息。
func (c *Consumer) markProcessed(msg kafka.Message) {
	c.updateTopicStats(msg, nil, func(stat *TopicStats) {
		stat.Processed++
		stat.LastProcessedAt = time.Now()
		stat.LastPartition = msg.Partition
		stat.LastOffset = msg.Offset
		stat.LastError = ""
	})
}

// markSkipped 记录主动跳过消息。
func (c *Consumer) markSkipped(msg kafka.Message, cause error) {
	c.updateTopicStats(msg, cause, func(stat *TopicStats) {
		stat.Skipped++
		stat.LastSkippedAt = time.Now()
		stat.LastPartition = msg.Partition
		stat.LastOffset = msg.Offset
	})
}

// markFailed 记录处理失败消息。
func (c *Consumer) markFailed(msg kafka.Message, cause error) {
	c.updateTopicStats(msg, cause, func(stat *TopicStats) {
		stat.Failed++
		stat.LastFailedAt = time.Now()
		stat.LastPartition = msg.Partition
		stat.LastOffset = msg.Offset
	})
}

// markDeadLettered 记录已进入死信消息。
func (c *Consumer) markDeadLettered(msg kafka.Message, cause error) {
	c.updateTopicStats(msg, cause, func(stat *TopicStats) {
		stat.DeadLettered++
		stat.LastDeadLetteredAt = time.Now()
		stat.LastPartition = msg.Partition
		stat.LastOffset = msg.Offset
	})
}

// updateTopicStats 更新 Topic 维度状态。
func (c *Consumer) updateTopicStats(msg kafka.Message, cause error, fn func(*TopicStats)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	stat := c.topicStats[msg.Topic]
	stat.Topic = msg.Topic
	if cause != nil {
		stat.LastError = strings.TrimSpace(cause.Error())
	}
	fn(&stat)
	c.topicStats[msg.Topic] = stat
}

// sleepAfterFailure 按配置退避，避免失败消息刷屏和打满下游。
func (c *Consumer) sleepAfterFailure() {
	delay := time.Duration(c.cfg.RetryBackoffSeconds) * time.Second
	if delay <= 0 {
		delay = defaultCDCRetryBackoff
	}
	select {
	case <-time.After(delay):
	case <-c.ctx.Done():
	}
}

// messageKey 返回 Kafka 位点键。
func messageKey(msg kafka.Message) string {
	return strings.TrimSpace(msg.Topic) + ":" + intString(msg.Partition) + ":" + int64String(msg.Offset)
}

// cloneBytes 复制消息内容，避免上游复用底层 buffer。
func cloneBytes(in []byte) []byte {
	if len(in) == 0 {
		return nil
	}
	return append([]byte(nil), in...)
}

// processor 返回指定表的处理器。
func (c *Consumer) processor(table string) Processor {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.processors[normalizeTable(table)]
}

// enabledTopics 过滤启用的 Topic 规则。
func enabledTopics(topics []config.CDCTopicConfig) []config.CDCTopicConfig {
	out := make([]config.CDCTopicConfig, 0, len(topics))
	for _, topic := range topics {
		if topic.Enabled && strings.TrimSpace(topic.Topic) != "" {
			out = append(out, topic)
		}
	}
	return out
}

// normalizeTopics 归一化 Topic 规则中的文本字段。
func normalizeTopics(topics []config.CDCTopicConfig) []config.CDCTopicConfig {
	out := make([]config.CDCTopicConfig, 0, len(topics))
	for _, topic := range topics {
		topic.Topic = strings.TrimSpace(topic.Topic)
		topic.Table = normalizeTable(topic.Table)
		out = append(out, topic)
	}
	return out
}

// normalizeTable 归一化 db.table 路由键。
func normalizeTable(table string) string {
	return strings.ToLower(strings.TrimSpace(table))
}

// normalizeStrings 过滤空字符串并保留原始顺序。
func normalizeStrings(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}
