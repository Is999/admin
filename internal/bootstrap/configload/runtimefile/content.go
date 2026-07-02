package runtimefile

import (
	"os"
	"strings"

	"github.com/Is999/go-utils/errors"
	yaml "go.yaml.in/yaml/v3"
)

// knownContent 从运行期外部配置中提取当前版本认识的顶层配置块。
// 未识别的顶层配置保留给其它版本或其它项目使用，不参与当前进程解析，也不会导致启动失败。
func knownContent(path string) ([]byte, map[string]struct{}, error) {
	root, err := yamlRootMapping(path)
	if err != nil {
		return nil, nil, errors.Tag(err)
	}
	keys := make(map[string]struct{})
	if root == nil {
		return []byte("{}\n"), keys, nil
	}
	knownKeys := sectionKeys()
	filtered := &yaml.Node{
		Kind:    yaml.MappingNode,
		Content: make([]*yaml.Node, 0, len(root.Content)),
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := strings.TrimSpace(root.Content[i].Value)
		if _, ok := knownKeys[key]; !ok {
			continue
		}
		keys[key] = struct{}{}
		filtered.Content = append(filtered.Content, root.Content[i], root.Content[i+1])
	}
	if len(filtered.Content) == 0 {
		return []byte("{}\n"), keys, nil
	}
	content, err := yaml.Marshal(filtered)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "提取运行期外部配置失败 file=%s", path)
	}
	return content, keys, nil
}

// yamlRootMapping 读取 YAML 文件根映射节点。
func yamlRootMapping(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "读取 YAML 配置失败 file=%s", path)
	}
	var node yaml.Node
	if err = yaml.Unmarshal(data, &node); err != nil {
		return nil, errors.Wrapf(err, "解析 YAML 配置失败 file=%s", path)
	}
	if len(node.Content) == 0 || node.Content[0].Kind != yaml.MappingNode {
		return nil, nil
	}
	return node.Content[0], nil
}
