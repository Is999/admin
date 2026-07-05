package configload

import (
	"os"
	"strings"

	"github.com/Is999/go-utils/errors"
	yaml "go.yaml.in/yaml/v3"
)

// rejectDeprecatedCollectorConfig 在 go-zero 配置解析前拒绝旧 Collector 多载体字段。
func rejectDeprecatedCollectorConfig(file string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return errors.Tag(err)
	}
	var node yaml.Node
	if err = yaml.Unmarshal(data, &node); err != nil {
		return errors.Wrap(err, "解析配置 YAML 失败")
	}
	if len(node.Content) == 0 || node.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	collector := yamlMappingValue(node.Content[0], "collector")
	if collector == nil || collector.Kind != yaml.MappingNode {
		return nil
	}
	deprecated := make([]string, 0, 4)
	for i := 0; i+1 < len(collector.Content); i += 2 {
		key := strings.TrimSpace(collector.Content[i].Value)
		switch key {
		case "transport", "consume_mode", "redis", "db":
			deprecated = append(deprecated, "collector."+key)
		case "kafka":
			deprecated = append(deprecated, deprecatedCollectorKafkaFields(collector.Content[i+1])...)
		}
	}
	if len(deprecated) == 0 {
		return nil
	}
	return errors.Errorf("Collector 配置包含已删除字段 %s；正常链路只支持 collector.kafka、collector.default_task、collector.tasks、collector.failure_retry 和 collector.idempotency", strings.Join(deprecated, ", "))
}

// deprecatedCollectorKafkaFields 返回 Collector Kafka 中已删除或未支持的字段。
func deprecatedCollectorKafkaFields(node *yaml.Node) []string {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	deprecated := make([]string, 0, 4)
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := strings.TrimSpace(node.Content[i].Value)
		if !collectorKafkaFieldAllowed(key) {
			deprecated = append(deprecated, "collector.kafka."+key)
		}
	}
	return deprecated
}

// collectorKafkaFieldAllowed 判断字段是否属于 Kafka 连接和 producer 公共参数。
func collectorKafkaFieldAllowed(key string) bool {
	switch key {
	case "brokers", "write_batch_size", "write_batch_wait_milliseconds", "write_timeout", "read_timeout":
		return true
	default:
		return false
	}
}

// yamlMappingValue 返回指定映射键对应的 YAML 节点。
func yamlMappingValue(root *yaml.Node, key string) *yaml.Node {
	if root == nil || root.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if strings.TrimSpace(root.Content[i].Value) == key {
			return root.Content[i+1]
		}
	}
	return nil
}
