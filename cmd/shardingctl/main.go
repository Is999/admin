package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/Is999/go-utils/errors"
)

const (
	// maxShardCount 是固定逻辑桶支持的最大物理分片数。
	maxShardCount = 1024
	// tableNameUser 是业务用户分片主表。
	tableNameUser = "user"
	// tableNameUserTag 是必须显式配置物理分片数的用户标签表。
	tableNameUserTag = "user_tag"
	// keyModeApplication 表示主键由应用保证全局唯一，Proxy 不生成主键。
	keyModeApplication = "application"
	// keyModeProxy 表示主键由 Proxy 的雪花算法生成。
	keyModeProxy = "proxy"
)

// ruleOptions 表示分片规则生成参数。
type ruleOptions struct {
	Database        string // Proxy 逻辑库名
	StorageUnits    string // 逗号分隔的目标存储单元
	UserShards      int    // user 物理分片数
	TableShards     string // 其余分片表的 table=count 列表
	ReferenceTables string // 与 user 节点布局完全一致的显式 reference 表
	KeyStrategies   string // 每张分片表的显式全局主键策略
}

// keyStrategy 表示一张分片表的全局主键来源。
type keyStrategy struct {
	Mode   string // application 或 proxy
	Column string // Proxy 雪花主键列；应用生成时为空
}

// tableRule 表示一张逻辑表的物理分片规则。
type tableRule struct {
	Name       string      // 逻辑表名
	ShardCount int         // 物理分片数
	Key        keyStrategy // 显式全局主键策略
}

// main 解析参数并输出可审查的 ShardingSphere DistSQL。
func main() {
	options := ruleOptions{}
	flag.StringVar(&options.Database, "database", "", "Proxy 逻辑库名")
	flag.StringVar(&options.StorageUnits, "storage-units", "", "目标存储单元，逗号分隔，例如 ds_0,ds_1")
	flag.IntVar(&options.UserShards, "user-shards", 1, "user 物理分片数：1/2/4/.../1024")
	flag.StringVar(&options.TableShards, "table-shards", "user_tag=1", "其余分片表及数量，格式 table=count,table=count")
	flag.StringVar(&options.ReferenceTables, "reference-tables", "", "与 user 节点布局完全一致且需要关联路由的表，逗号分隔")
	flag.StringVar(&options.KeyStrategies, "key-strategies", "user=application,user_tag=proxy:id", "每张分片表的全局主键策略，格式 table=application 或 table=proxy:column")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "存在未识别参数 %q\n", flag.Arg(0))
		os.Exit(1)
	}
	if err := run(options, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run 校验规则并输出 DistSQL，不直接连接或修改生产 Proxy。
func run(options ruleOptions, output io.Writer) error {
	if output == nil {
		return errors.New("输出目标不能为空")
	}
	database, err := parseIdentifier(options.Database, "逻辑库名")
	if err != nil {
		return errors.Tag(err)
	}
	storageUnits, err := parseIdentifierList(options.StorageUnits, "存储单元")
	if err != nil {
		return errors.Tag(err)
	}
	if err := validateShardCount(options.UserShards); err != nil {
		return errors.Wrap(err, "user 分片数错误")
	}
	tableRules, err := parseTableRules(options.TableShards)
	if err != nil {
		return errors.Tag(err)
	}
	referenceTables, err := parseIdentifierListAllowEmpty(options.ReferenceTables, "reference 表")
	if err != nil {
		return errors.Tag(err)
	}

	seen := map[string]string{tableNameUser: "user 主表"}
	hasUserTag := false
	for _, rule := range tableRules {
		if source, exists := seen[rule.Name]; exists {
			return errors.Errorf("表 %s 重复定义，已属于%s", rule.Name, source)
		}
		if rule.Name == tableNameUserTag {
			hasUserTag = true
		}
		seen[rule.Name] = "分片表"
	}
	if !hasUserTag {
		return errors.New("分片表必须包含 user_tag 及其独立物理分片数")
	}
	ruleShards := make(map[string]int, len(tableRules))
	for _, rule := range tableRules {
		ruleShards[rule.Name] = rule.ShardCount
	}
	for _, table := range referenceTables {
		shardCount, exists := ruleShards[table]
		if !exists {
			return errors.Errorf("reference 表 %s 未定义物理分片数", table)
		}
		if shardCount != options.UserShards {
			return errors.Errorf("reference 表 %s 的分片数 %d 与 user 的 %d 不一致", table, shardCount, options.UserShards)
		}
	}

	rules := make([]tableRule, 0, 1+len(tableRules))
	rules = append(rules, tableRule{Name: tableNameUser, ShardCount: options.UserShards})
	rules = append(rules, tableRules...)
	if err := applyKeyStrategies(rules, options.KeyStrategies); err != nil {
		return errors.Tag(err)
	}
	if err := validateStorageLayout(storageUnits, rules); err != nil {
		return errors.Tag(err)
	}
	return writeRules(output, database, storageUnits, rules, referenceTables)
}

// validateStorageLayout 校验自动分片落点不会出现未声明的不均衡或闲置节点。
func validateStorageLayout(storageUnits []string, rules []tableRule) error {
	count := len(storageUnits)
	if err := validateShardCount(count); err != nil {
		return errors.Wrap(err, "存储单元数量错误")
	}
	maxTableShards := 0
	for _, rule := range rules {
		if rule.ShardCount > maxTableShards {
			maxTableShards = rule.ShardCount
		}
		if rule.ShardCount >= count && rule.ShardCount%count != 0 {
			return errors.Errorf("表 %s 的 %d 个分片无法均匀分配到 %d 个存储单元", rule.Name, rule.ShardCount, count)
		}
	}
	if maxTableShards < count {
		return errors.Errorf("最多只有 %d 个物理分片，无法覆盖 %d 个存储单元", maxTableShards, count)
	}
	return nil
}

// writeRules 输出逻辑表规则、显式 reference 关系和只读核验语句。
func writeRules(output io.Writer, database string, storageUnits []string, rules []tableRule, referenceTables []string) error {
	if _, err := fmt.Fprintf(output, "USE %s;\n\n", quoteIdentifier(database)); err != nil {
		return errors.Wrap(err, "输出逻辑库规则失败")
	}
	storageSQL := quoteIdentifiers(storageUnits)
	if _, err := fmt.Fprintf(output, "SET DEFAULT SINGLE TABLE STORAGE UNIT = %s;\n\n", storageSQL[0]); err != nil {
		return errors.Wrap(err, "输出默认单表规则失败")
	}
	for _, rule := range rules {
		// ShardingSphere 5.5.3 的 DistSQL 规则名不接受反引号；表名已通过 parseIdentifier 严格校验。
		if _, err := fmt.Fprintf(output, "CREATE SHARDING TABLE RULE %s (\n", rule.Name); err != nil {
			return errors.Wrapf(err, "输出表 %s 分片规则失败", rule.Name)
		}
		if _, err := fmt.Fprintf(output, "  STORAGE_UNITS(%s),\n", strings.Join(storageSQL, ",")); err != nil {
			return errors.Wrapf(err, "输出表 %s 存储单元失败", rule.Name)
		}
		if _, err := fmt.Fprintf(output, "  SHARDING_COLUMN=shard_no,\n  TYPE(NAME=\"MOD\",PROPERTIES(\"sharding-count\"=\"%d\")),\n", rule.ShardCount); err != nil {
			return errors.Wrapf(err, "输出表 %s 分片算法失败", rule.Name)
		}
		if rule.Key.Mode == keyModeProxy {
			if _, err := fmt.Fprintf(output, "  KEY_GENERATE_STRATEGY(COLUMN=%s,TYPE(NAME=\"SNOWFLAKE\")),\n", quoteIdentifier(rule.Key.Column)); err != nil {
				return errors.Wrapf(err, "输出表 %s 全局主键规则失败", rule.Name)
			}
		}
		if _, err := fmt.Fprint(output, "  AUDIT_STRATEGY(TYPE(NAME=\"DML_SHARDING_CONDITIONS\"),ALLOW_HINT_DISABLE=false)\n);\n"); err != nil {
			return errors.Wrapf(err, "输出表 %s 写入审计规则失败", rule.Name)
		}
	}
	if len(referenceTables) > 0 {
		tables := append([]string{tableNameUser}, referenceTables...)
		if _, err := fmt.Fprintf(output, "\nCREATE SHARDING TABLE REFERENCE RULE user_reference (%s);\n", strings.Join(quoteIdentifiers(tables), ",")); err != nil {
			return errors.Wrap(err, "输出 user reference 关系失败")
		}
	}
	if _, err := fmt.Fprint(output, "\nSHOW DEFAULT SINGLE TABLE STORAGE UNIT;\nSHOW SHARDING TABLE RULES;\nSHOW SHARDING TABLE NODES;\nSHOW SHARDING TABLE REFERENCE RULES;\nSHOW SHARDING KEY GENERATORS;\n"); err != nil {
		return errors.Wrap(err, "输出规则核验语句失败")
	}
	return nil
}

// applyKeyStrategies 要求每张分片表显式声明全局主键来源，避免物理自增键跨分片冲突。
func applyKeyStrategies(rules []tableRule, value string) error {
	strategies, err := parseKeyStrategies(value)
	if err != nil {
		return err
	}
	defined := make(map[string]struct{}, len(rules))
	for index := range rules {
		rule := &rules[index]
		defined[rule.Name] = struct{}{}
		strategy, exists := strategies[rule.Name]
		if !exists {
			return errors.Errorf("表 %s 缺少全局主键策略", rule.Name)
		}
		if rule.Name == tableNameUser && strategy.Mode != keyModeApplication {
			return errors.New("user 必须使用 application 主键策略，保持现有雪花 ID 契约")
		}
		if rule.Name == tableNameUserTag && (strategy.Mode != keyModeProxy || strategy.Column != "id") {
			return errors.New("user_tag 必须使用 proxy:id 主键策略，禁止依赖物理表自增唯一性")
		}
		rule.Key = strategy
	}
	for table := range strategies {
		if _, exists := defined[table]; !exists {
			return errors.Errorf("全局主键策略包含未定义分片表 %s", table)
		}
	}
	return nil
}

// parseKeyStrategies 解析 table=application 或 table=proxy:column，并拒绝隐式默认值。
func parseKeyStrategies(value string) (map[string]keyStrategy, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("全局主键策略不能为空")
	}
	items := strings.Split(value, ",")
	strategies := make(map[string]keyStrategy, len(items))
	for _, item := range items {
		tableText, strategyText, found := strings.Cut(strings.TrimSpace(item), "=")
		if !found || strings.Contains(strategyText, "=") {
			return nil, errors.Errorf("全局主键策略格式错误 %q", item)
		}
		table, err := parseIdentifier(tableText, "全局主键策略表名")
		if err != nil {
			return nil, err
		}
		if _, exists := strategies[table]; exists {
			return nil, errors.Errorf("表 %s 的全局主键策略重复定义", table)
		}
		strategyText = strings.TrimSpace(strategyText)
		strategy := keyStrategy{Mode: keyModeApplication}
		if strategyText != keyModeApplication {
			prefix := keyModeProxy + ":"
			if !strings.HasPrefix(strategyText, prefix) {
				return nil, errors.Errorf("表 %s 的全局主键策略必须是 application 或 proxy:column", table)
			}
			column, parseErr := parseIdentifier(strings.TrimPrefix(strategyText, prefix), "Proxy主键列")
			if parseErr != nil {
				return nil, parseErr
			}
			strategy = keyStrategy{Mode: keyModeProxy, Column: column}
		}
		strategies[table] = strategy
	}
	return strategies, nil
}

// parseTableRules 解析 table=count，并按表名排序保证计划稳定。
func parseTableRules(value string) ([]tableRule, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("分片表及数量不能为空")
	}
	items := strings.Split(value, ",")
	rules := make([]tableRule, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		parts := strings.Split(strings.TrimSpace(item), "=")
		if len(parts) != 2 {
			return nil, errors.Errorf("分片表格式错误 %q，应为 table=count", item)
		}
		name, err := parseIdentifier(parts[0], "分片表名")
		if err != nil {
			return nil, err
		}
		if _, exists := seen[name]; exists {
			return nil, errors.Errorf("分片表 %s 重复定义", name)
		}
		count, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return nil, errors.Errorf("表 %s 的分片数不是整数", name)
		}
		if err := validateShardCount(count); err != nil {
			return nil, errors.Wrapf(err, "表 %s 分片数错误", name)
		}
		seen[name] = struct{}{}
		rules = append(rules, tableRule{Name: name, ShardCount: count})
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].Name < rules[j].Name })
	return rules, nil
}

// parseIdentifierList 解析非空、去重的标识符列表。
func parseIdentifierList(value string, field string) ([]string, error) {
	items, err := parseIdentifierListAllowEmpty(value, field)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.Errorf("%s不能为空", field)
	}
	return items, nil
}

// parseIdentifierListAllowEmpty 解析可为空、去重的标识符列表。
func parseIdentifierListAllowEmpty(value string, field string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		item, err := parseIdentifier(part, field)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[item]; exists {
			return nil, errors.Errorf("%s %s 重复", field, item)
		}
		seen[item] = struct{}{}
		items = append(items, item)
	}
	return items, nil
}

// parseIdentifier 校验 DistSQL 标识符，禁止外部文本进入 SQL 语法。
func parseIdentifier(value string, field string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 {
		return "", errors.Errorf("%s不能为空且长度不能超过64", field)
	}
	for index, r := range value {
		if r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || index > 0 && r >= '0' && r <= '9' {
			continue
		}
		return "", errors.Errorf("%s包含非法标识符 %q", field, value)
	}
	return value, nil
}

// validateShardCount 校验物理分片数是 1-1024 内的 2 的幂。
func validateShardCount(count int) error {
	if count < 1 || count > maxShardCount || count&(count-1) != 0 {
		return errors.Errorf("仅支持 1/2/4/8/.../%d", maxShardCount)
	}
	return nil
}

// quoteIdentifier 为已校验标识符补充反引号。
func quoteIdentifier(value string) string {
	return "`" + value + "`"
}

// quoteIdentifiers 批量引用已校验标识符。
func quoteIdentifiers(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, quoteIdentifier(value))
	}
	return out
}
