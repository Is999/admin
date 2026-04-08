package excel

// OrderedIDShard 表示按有序整型主键切分的导出分片。
type OrderedIDShard struct {
	StartID int // 分片起始 ID（包含）
	EndID   int // 分片结束 ID（包含）；0 表示无上界
}

// RecommendShardCount 根据总量、批量大小和最大并发度推荐分片数。
func RecommendShardCount(total int64, batchSize int, maxConcurrentShards int, pagesPerShard int) int {
	if total <= 0 {
		return 1
	}
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	if maxConcurrentShards <= 0 {
		maxConcurrentShards = DefaultMaxWorkerCount
	}
	if pagesPerShard <= 0 {
		pagesPerShard = 2
	}
	shardCount := int(total / int64(batchSize*pagesPerShard))
	if shardCount < 1 {
		shardCount = 1
	}
	if shardCount > maxConcurrentShards {
		shardCount = maxConcurrentShards
	}
	if total < int64(shardCount) {
		shardCount = int(total)
	}
	if shardCount <= 0 {
		return 1
	}
	return shardCount
}

// BuildOrderedIDShards 按主键范围等分构造顺序分片。
func BuildOrderedIDShards(minID int, maxID int, shardCount int) []ExportShard[OrderedIDShard] {
	if minID <= 0 || maxID <= 0 || minID >= maxID || shardCount <= 1 {
		return []ExportShard[OrderedIDShard]{
			{Meta: OrderedIDShard{StartID: max(minID, 1), EndID: 0}},
		}
	}
	rangeSize := (maxID - minID + 1 + shardCount - 1) / shardCount
	if rangeSize <= 0 {
		rangeSize = maxID - minID + 1
	}
	shards := make([]ExportShard[OrderedIDShard], 0, shardCount)
	for index := 0; index < shardCount; index++ {
		startID := minID + index*rangeSize
		if startID > maxID {
			break
		}
		endID := startID + rangeSize - 1
		if index == shardCount-1 || endID > maxID {
			endID = maxID
		}
		shard := OrderedIDShard{StartID: startID, EndID: endID}
		if index == shardCount-1 {
			shard.EndID = 0
		}
		shards = append(shards, ExportShard[OrderedIDShard]{Meta: shard})
	}
	if len(shards) == 0 {
		return []ExportShard[OrderedIDShard]{
			{Meta: OrderedIDShard{StartID: max(minID, 1), EndID: 0}},
		}
	}
	return shards
}
