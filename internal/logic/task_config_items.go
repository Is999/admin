package logic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	codes "admin_cron/common/codes"
	i18n "admin_cron/common/i18n"
	"admin_cron/internal/config"
	"admin_cron/internal/taskqueue"
	"admin_cron/internal/types"

	yaml "go.yaml.in/yaml/v3"
)

const (
	taskConfigDefaultPage     = 1
	taskConfigMaskPlaceholder = "****"
	taskConfigValueBool       = "bool"
	taskConfigValueList       = "list"
	taskConfigValueNull       = "null"
	taskConfigValueNumber     = "number"
	taskConfigValueObject     = "object"
	taskConfigValueString     = "string"
)

type maskedTaskConfigView struct {
	items          []types.TaskConfigItem
	sections       []types.TaskConfigSectionStat
	sensitiveTotal int
	snapshotYAML   string
}

var (
	taskConfigIPv4Pattern = regexp.MustCompile(`(?:^|[^\d])(?:(?:25[0-5]|2[0-4]\d|1?\d?\d)\.){3}(?:25[0-5]|2[0-4]\d|1?\d?\d)(?:$|[^\d])`)
	taskConfigHostPattern = regexp.MustCompile(`(?i)\b(?:localhost|(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{1,62}):\d{2,5}\b`)
)

// GetConfigReloadItems 查询当前运行态配置快照中的配置项。
// 数据来源必须是 ServiceContext.CurrentConfig，确保页面看到的是热加载后已经生效的配置快照。
func (l *TaskLogic) GetConfigReloadItems(req *types.TaskConfigItemQueryReq) *types.BizResult {
	if l.svc == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(wrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.GetConfigReloadItems 配置热加载未启用"))
	}
	if req == nil {
		req = &types.TaskConfigItemQueryReq{}
	}
	if err := req.Validate(); err != nil {
		return types.ParamErrorResult(err)
	}

	currentConfig := l.svc.CurrentConfig()
	view, err := buildMaskedTaskConfigView(currentConfig)
	if err != nil {
		return types.ServerError(i18n.MsgKeyTaskQueryFail, err, "TaskLogic.GetConfigReloadItems").ToBizResult()
	}
	runtimeYAML, err := buildMaskedTaskConfigRuntimeYAML(currentConfig)
	if err != nil {
		return types.ServerError(i18n.MsgKeyTaskQueryFail, err, "TaskLogic.GetConfigReloadItems.runtime").ToBizResult()
	}
	filtered := filterTaskConfigItems(view.items, req.Keyword, req.SensitiveOnly)
	pageItems := paginateTaskConfigItems(filtered, req.Page, req.PageSize)
	status := l.svc.CurrentHotReloadStatus()

	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(&types.TaskConfigItemQueryResp{
			Keyword:        req.Keyword,
			SensitiveOnly:  req.SensitiveOnly,
			Page:           req.Page,
			PageSize:       req.PageSize,
			Total:          int64(len(filtered)),
			TotalItems:     len(view.items),
			SensitiveTotal: view.sensitiveTotal,
			Sections:       view.sections,
			Source: types.TaskConfigSourceMeta{
				Source:            "runtime_snapshot",
				ConfigFile:        status.ConfigFile,
				RuntimeFile:       taskConfigRuntimeFilePath(status.ConfigFile, currentConfig.ConfigFiles.Runtime),
				ConfigVersion:     status.ConfigVersion,
				LastStatus:        status.LastStatus,
				LastTriggerSource: status.LastTriggerSource,
				LastReloadAt:      formatTaskTime(status.LastReloadAt),
				LastSuccessAt:     formatTaskTime(status.LastSuccessAt),
				RestartRequired:   status.RestartRequired,
				RestartReason:     status.RestartReason,
			},
			SnapshotYAML: view.snapshotYAML,
			RuntimeYAML:  runtimeYAML,
			Items:        pageItems,
		})
}

// buildMaskedTaskConfigView 将配置结构按 json tag 转为稳定的扁平路径和脱敏 YAML 快照。
func buildMaskedTaskConfigView(cfg config.Config) (*maskedTaskConfigView, error) {
	payload, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()

	var root any
	if err = decoder.Decode(&root); err != nil {
		return nil, err
	}

	items := make([]types.TaskConfigItem, 0, 128)
	snapshot := appendTaskConfigItems(&items, "", root, false)
	sort.Slice(items, func(i, j int) bool {
		return items[i].Path < items[j].Path
	})
	snapshotYAML, err := marshalTaskConfigSnapshotYAML(snapshot, true)
	if err != nil {
		return nil, err
	}
	return &maskedTaskConfigView{
		items:          items,
		sections:       buildTaskConfigSectionStats(items),
		sensitiveTotal: countSensitiveTaskConfigItems(items),
		snapshotYAML:   snapshotYAML,
	}, nil
}

// buildMaskedTaskConfigRuntimeYAML 按 runtime.yaml 原始顶层结构展示已并入运行态的外部配置。
func buildMaskedTaskConfigRuntimeYAML(cfg config.Config) (string, error) {
	view := make(map[string]any, 3)
	if len(cfg.Archive.Jobs) > 0 {
		view["archive_jobs"] = cfg.Archive.Jobs
	}
	if len(cfg.Task.Periodic) > 0 {
		view["task_periodic"] = cfg.Task.Periodic
	}
	if !reflect.DeepEqual(cfg.Workflows, config.WorkflowsConfig{}) {
		view["workflows"] = cfg.Workflows
	}
	return buildMaskedTaskConfigYAML(view, true)
}

// buildMaskedTaskConfigYAML 将指定配置块转换为脱敏后的 YAML 文本。
func buildMaskedTaskConfigYAML(value any, omitZeroNumbers bool) (string, error) {
	root, err := decodeTaskConfigJSONValue(value)
	if err != nil {
		return "", err
	}
	items := make([]types.TaskConfigItem, 0, 32)
	snapshot := appendTaskConfigItems(&items, "", root, false)
	return marshalTaskConfigSnapshotYAML(snapshot, omitZeroNumbers)
}

// decodeTaskConfigJSONValue 通过 JSON 中间形态统一结构体 tag 和数字类型。
func decodeTaskConfigJSONValue(value any) (any, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()

	var root any
	if err = decoder.Decode(&root); err != nil {
		return nil, err
	}
	return root, nil
}

// appendTaskConfigItems 深度展开配置树；配置 key 原样保留，只有 value 按敏感规则脱敏。
func appendTaskConfigItems(items *[]types.TaskConfigItem, path string, value any, inheritedSensitive bool) any {
	currentSensitive := inheritedSensitive || isSensitiveConfigPath(path) || isAddressConfigPath(path)
	switch typed := value.(type) {
	case map[string]any:
		if len(typed) == 0 {
			appendTaskConfigLeaf(items, path, taskConfigValueObject, "{}", currentSensitive)
			return map[string]any{}
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		maskedMap := make(map[string]any, len(typed))
		for _, key := range keys {
			maskedMap[key] = appendTaskConfigItems(items, joinTaskConfigPath(path, key), typed[key], currentSensitive)
		}
		return maskedMap
	case []any:
		if len(typed) == 0 {
			appendTaskConfigLeaf(items, path, taskConfigValueList, "[]", currentSensitive)
			return []any{}
		}
		maskedList := make([]any, 0, len(typed))
		for index, item := range typed {
			maskedList = append(maskedList, appendTaskConfigItems(items, fmt.Sprintf("%s[%d]", path, index), item, currentSensitive))
		}
		return maskedList
	default:
		item := buildTaskConfigLeaf(path, taskConfigValueType(value), formatTaskConfigValue(value), currentSensitive)
		appendTaskConfigLeafItem(items, item)
		return taskConfigYAMLLeafValue(value, item)
	}
}

// appendTaskConfigLeaf 写入最终叶子节点，敏感值只保留首尾少量字符。
func appendTaskConfigLeaf(items *[]types.TaskConfigItem, path string, valueType string, rawValue string, inheritedSensitive bool) {
	appendTaskConfigLeafItem(items, buildTaskConfigLeaf(path, valueType, rawValue, inheritedSensitive))
}

// buildTaskConfigLeaf 构造单个配置叶子节点，并在必要时脱敏展示值。
func buildTaskConfigLeaf(path string, valueType string, rawValue string, inheritedSensitive bool) types.TaskConfigItem {
	if strings.TrimSpace(path) == "" {
		return types.TaskConfigItem{}
	}
	sensitive := inheritedSensitive || shouldMaskTaskConfigValue(path, rawValue)
	displayValue := rawValue
	if sensitive && isMaskableTaskConfigValue(rawValue) {
		displayValue = maskTaskConfigString(rawValue)
	}
	return types.TaskConfigItem{
		Path:      path,
		Value:     displayValue,
		ValueType: valueType,
		Sensitive: sensitive,
	}
}

// appendTaskConfigLeafItem 追加有效叶子节点，根节点空路径不参与展示。
func appendTaskConfigLeafItem(items *[]types.TaskConfigItem, item types.TaskConfigItem) {
	if strings.TrimSpace(item.Path) == "" {
		return
	}
	*items = append(*items, item)
}

// filterTaskConfigItems 只使用已脱敏展示值参与匹配，避免通过关键字搜索探测原始敏感值。
func filterTaskConfigItems(items []types.TaskConfigItem, keyword string, sensitiveOnly bool) []types.TaskConfigItem {
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	filtered := make([]types.TaskConfigItem, 0, len(items))
	for _, item := range items {
		if sensitiveOnly && !item.Sensitive {
			continue
		}
		if keyword != "" && !taskConfigItemMatches(item, keyword) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

// taskConfigItemMatches 使用已脱敏字段做关键字匹配，避免命中原始敏感值。
func taskConfigItemMatches(item types.TaskConfigItem, keyword string) bool {
	return strings.Contains(strings.ToLower(item.Path), keyword) ||
		strings.Contains(strings.ToLower(item.ValueType), keyword) ||
		strings.Contains(strings.ToLower(item.Value), keyword)
}

// paginateTaskConfigItems 对内存中的已脱敏配置项做安全分页。
func paginateTaskConfigItems(items []types.TaskConfigItem, page int, pageSize int) []types.TaskConfigItem {
	if page <= 0 {
		page = taskConfigDefaultPage
	}
	if pageSize <= 0 {
		pageSize = len(items)
	}
	start := (page - 1) * pageSize
	if start >= len(items) {
		return []types.TaskConfigItem{}
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}

// marshalTaskConfigSnapshotYAML 输出紧凑 YAML，避免默认零值把页面撑得不可读。
func marshalTaskConfigSnapshotYAML(snapshot any, omitZeroNumbers bool) (string, error) {
	compactSnapshot, ok := compactTaskConfigYAMLValue(snapshot, omitZeroNumbers)
	if !ok {
		return "", nil
	}
	payload, err := yaml.Marshal(compactSnapshot)
	if err != nil {
		return "", err
	}
	text := strings.TrimRight(string(payload), "\n")
	if text == "" {
		return "", nil
	}
	return text + "\n", nil
}

// compactTaskConfigYAMLValue 递归移除空值；运行态快照可按需隐藏数字零值。
func compactTaskConfigYAMLValue(value any, omitZeroNumbers bool) (any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			child, ok := compactTaskConfigYAMLValue(typed[key], omitZeroNumbers)
			if ok {
				result[key] = child
			}
		}
		return result, len(result) > 0
	case []any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			child, ok := compactTaskConfigYAMLValue(item, omitZeroNumbers)
			if ok {
				result = append(result, child)
			}
		}
		return result, len(result) > 0
	case string:
		return typed, strings.TrimSpace(typed) != ""
	case json.Number:
		if omitZeroNumbers && typed.String() == "0" {
			return nil, false
		}
		return taskConfigNumberYAMLValue(typed), true
	case nil:
		return nil, false
	default:
		return typed, true
	}
}

// buildTaskConfigSectionStats 汇总顶层配置块数量和敏感项数量。
func buildTaskConfigSectionStats(items []types.TaskConfigItem) []types.TaskConfigSectionStat {
	statsByName := make(map[string]*types.TaskConfigSectionStat)
	for _, item := range items {
		name := taskConfigRootSection(item.Path)
		stat := statsByName[name]
		if stat == nil {
			stat = &types.TaskConfigSectionStat{Name: name}
			statsByName[name] = stat
		}
		stat.Total++
		if item.Sensitive {
			stat.SensitiveTotal++
		}
	}
	sections := make([]types.TaskConfigSectionStat, 0, len(statsByName))
	for _, stat := range statsByName {
		sections = append(sections, *stat)
	}
	sort.Slice(sections, func(i, j int) bool {
		return sections[i].Name < sections[j].Name
	})
	return sections
}

// countSensitiveTaskConfigItems 统计后端判定需要脱敏的叶子配置项数量。
func countSensitiveTaskConfigItems(items []types.TaskConfigItem) int {
	total := 0
	for _, item := range items {
		if item.Sensitive {
			total++
		}
	}
	return total
}

// taskConfigRootSection 提取配置路径的顶层块名称。
func taskConfigRootSection(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "root"
	}
	index := strings.IndexAny(path, ".[")
	if index < 0 {
		return path
	}
	return path[:index]
}

// taskConfigRuntimeFilePath 按主配置文件位置解析运行期配置文件路径。
func taskConfigRuntimeFilePath(configFile string, runtimeFile string) string {
	runtimeFile = strings.TrimSpace(runtimeFile)
	if runtimeFile == "" || filepath.IsAbs(runtimeFile) {
		return runtimeFile
	}
	configFile = strings.TrimSpace(configFile)
	if configFile == "" {
		return runtimeFile
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filepath.Clean(configFile)), runtimeFile))
}

// joinTaskConfigPath 拼接对象路径或数组下标路径。
func joinTaskConfigPath(parent string, child string) string {
	if parent == "" {
		return child
	}
	if strings.HasPrefix(child, "[") {
		return parent + child
	}
	return parent + "." + child
}

// shouldMaskTaskConfigValue 判断单个配置值是否需要脱敏展示。
func shouldMaskTaskConfigValue(path string, value string) bool {
	return isSensitiveConfigPath(path) ||
		isAddressConfigPath(path) ||
		isTaskConfigAddressLike(value)
}

// isSensitiveConfigPath 判断路径叶子名称是否属于密钥、密码或 Token 类字段。
func isSensitiveConfigPath(path string) bool {
	leaf := taskConfigPathLeaf(path)
	if leaf == "" {
		return false
	}
	switch leaf {
	case "access_key", "aes_iv", "aes_iv_ref", "aes_key", "aes_key_ref", "app_key",
		"jwt_secret", "password", "passwd", "private_key", "public_key", "pwd",
		"secret", "secret_key", "secret_ref", "token", "webhook_url", "webhook_url_ref":
		return true
	case "key_version", "stable_version", "gray_version", "version":
		return false
	}
	for _, token := range []string{"password", "passwd", "private_key", "public_key", "secret", "token"} {
		if strings.Contains(leaf, token) {
			return true
		}
	}
	return strings.Contains(leaf, "salt")
}

// isAddressConfigPath 判断路径叶子名称是否属于地址、连接串或 URL 类字段。
func isAddressConfigPath(path string) bool {
	leaf := taskConfigPathLeaf(path)
	if leaf == "" {
		return false
	}
	switch leaf {
	case "addr", "addr_map", "addrs", "address", "addresses", "broker", "brokers",
		"data_source", "datasource", "domain", "dsn", "endpoint", "endpoints",
		"host", "hosts", "read_data_sources", "uri", "url", "webhook_url",
		"write_data_source":
		return true
	}
	for _, token := range []string{"_addr", "_address", "_broker", "_data_source", "_datasource", "_domain", "_dsn", "_endpoint", "_host", "_uri", "_url"} {
		if strings.Contains(leaf, token) {
			return true
		}
	}
	return false
}

// taskConfigPathLeaf 归一化配置路径叶子，便于敏感规则匹配。
func taskConfigPathLeaf(path string) string {
	leaf := strings.TrimSpace(path)
	for strings.HasSuffix(leaf, "]") {
		index := strings.LastIndex(leaf, "[")
		if index < 0 {
			break
		}
		leaf = leaf[:index]
	}
	if dot := strings.LastIndex(leaf, "."); dot >= 0 {
		leaf = leaf[dot+1:]
	}
	leaf = strings.ToLower(strings.TrimSpace(leaf))
	leaf = strings.ReplaceAll(leaf, "-", "_")
	return leaf
}

// isTaskConfigAddressLike 识别常见 URL、host:port、IP 和数据库 DSN。
func isTaskConfigAddressLike(value string) bool {
	text := strings.TrimSpace(value)
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	if strings.Contains(lower, "@tcp(") || strings.Contains(lower, "@udp(") {
		return true
	}
	if parsed, err := url.Parse(text); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		return true
	}
	if host, _, err := net.SplitHostPort(text); err == nil && strings.TrimSpace(host) != "" {
		return true
	}
	return taskConfigIPv4Pattern.MatchString(text) ||
		taskConfigHostPattern.MatchString(text)
}

// isMaskableTaskConfigValue 判断当前展示值是否适合做首尾保留脱敏。
func isMaskableTaskConfigValue(value string) bool {
	trimmed := strings.TrimSpace(value)
	switch trimmed {
	case "", "[]", "{}", "null":
		return false
	default:
		return true
	}
}

// maskTaskConfigString 对敏感字符串做首尾保留脱敏，避免泄露地址和密钥原文。
func maskTaskConfigString(value string) string {
	runes := []rune(strings.TrimSpace(value))
	switch {
	case len(runes) == 0:
		return ""
	case len(runes) <= 4:
		return taskConfigMaskPlaceholder
	case len(runes) <= 8:
		return string(runes[:1]) + taskConfigMaskPlaceholder + string(runes[len(runes)-1:])
	case len(runes) <= 16:
		return string(runes[:2]) + taskConfigMaskPlaceholder + string(runes[len(runes)-2:])
	default:
		return string(runes[:4]) + taskConfigMaskPlaceholder + string(runes[len(runes)-4:])
	}
}

// taskConfigYAMLLeafValue 返回 YAML 快照中的叶子值，敏感项使用脱敏展示值。
func taskConfigYAMLLeafValue(value any, item types.TaskConfigItem) any {
	if item.Sensitive {
		return item.Value
	}
	switch typed := value.(type) {
	case json.Number:
		return taskConfigNumberYAMLValue(typed)
	default:
		return value
	}
}

// taskConfigNumberYAMLValue 将 JSON 数字还原为 YAML 友好的整数或浮点数。
func taskConfigNumberYAMLValue(value json.Number) any {
	if numberValue, err := value.Int64(); err == nil {
		return numberValue
	}
	if numberValue, err := value.Float64(); err == nil {
		return numberValue
	}
	return value.String()
}

// taskConfigValueType 返回前端用于筛选和展示的稳定值类型。
func taskConfigValueType(value any) string {
	switch value.(type) {
	case nil:
		return taskConfigValueNull
	case bool:
		return taskConfigValueBool
	case json.Number, float32, float64, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return taskConfigValueNumber
	case []any:
		return taskConfigValueList
	case map[string]any:
		return taskConfigValueObject
	default:
		return taskConfigValueString
	}
}

// formatTaskConfigValue 将叶子配置值转成表格展示字符串。
func formatTaskConfigValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	case json.Number:
		return typed.String()
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return fmt.Sprint(value)
	}
}
