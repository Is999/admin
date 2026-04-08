package logic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"

	tablecache "github.com/Is999/table-cache"

	"admin_cron/common/codes"
	i18n "admin_cron/common/i18n"
	keys "admin_cron/common/rediskeys"
	"admin_cron/internal/model"
	"admin_cron/internal/svc"
	"admin_cron/internal/types"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// SysConfigLogic 承载系统常量配置的查询、保存和缓存刷新逻辑。
type SysConfigLogic struct {
	*BaseLogic // 复用上下文、数据库、Redis 和日志能力
}

// NewSysConfigLogic 创建系统常量配置业务逻辑对象。
func NewSysConfigLogic(r *http.Request, svcCtx *svc.ServiceContext) *SysConfigLogic {
	return &SysConfigLogic{
		BaseLogic: NewBaseLogic(r, svcCtx),
	}
}

// List 分页查询系统常量配置。
func (l *SysConfigLogic) List(req *types.SysConfigListReq) *types.BizResult {
	dbq := l.svc.ReadDB(svc.DatabaseMain).Model(&model.SysConfig{})
	if req.UUID != "" {
		dbq = dbq.Where("uuid = ?", req.UUID)
	}
	if req.Title != "" {
		dbq = dbq.Where("title LIKE ?", "%"+req.Title+"%")
	}
	if req.PagePath != "" {
		dbq = dbq.Where("page = ?", req.PagePath)
	}

	orderBy := normalizedOrderField(req.OrderBy, "id")
	list, total, err := model.List[model.SysConfig](dbq, req.GetPageReq.Page, req.PageSize, orderBy, normalizedOrderDirection(req.Order))
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"SysConfigLogic.List 查询系统配置列表失败").ToBizResult()
	}

	items := make([]types.SysConfigItem, 0, len(list))
	for _, cfg := range list {
		items = append(items, sysConfigModelToItem(cfg))
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(types.ListResp[types.SysConfigItem]{List: items, Total: total})
}

// Create 新增系统常量配置，并刷新对应缓存。
func (l *SysConfigLogic) Create(req *types.SaveSysConfigReq) *types.BizResult {
	valueRaw, err := req.ValueRawMessage()
	if err != nil {
		return types.NewBizResult(codes.ParamError).
			SetI18nMessage(i18n.MsgKeyParamErrorFormat, err.Error()).
			WithError(wrapLogicError(err, "SysConfigLogic.Create 参数校验失败"))
	}
	exampleRaw, err := req.ExampleRawMessage()
	if err != nil {
		return types.NewBizResult(codes.ParamError).
			SetI18nMessage(i18n.MsgKeyParamErrorFormat, err.Error()).
			WithError(wrapLogicError(err, "SysConfigLogic.Create 参数校验失败"))
	}
	value, example, err := normalizeSysConfigJSON(req.Type, valueRaw, exampleRaw)
	if err != nil {
		return types.NewBizResult(codes.ParamError).
			SetI18nMessage(i18n.MsgKeyParamErrorFormat, err.Error()).
			WithError(wrapLogicError(err, "SysConfigLogic.Create 参数校验失败"))
	}

	cfg := model.SysConfig{
		UUID:      req.UUID,
		Title:     req.Title,
		Type:      req.Type,
		Value:     value,
		Example:   example,
		Remark:    req.Remark,
		Page:      req.Page,
		Pid:       req.Pid,
		Version:   0,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err = l.svc.WriteDB(svc.DatabaseMain).Transaction(func(tx *gorm.DB) error {
		pids, err := l.sysConfigPidsTx(tx, req.Pid, 0)
		if err != nil {
			return errors.Tag(err)
		}
		cfg.Pids = pids
		if err := l.ensureSysConfigUUIDUniqueTx(tx, req.UUID, 0); err != nil {
			return errors.Tag(err)
		}
		if err := tx.Create(&cfg).Error; err != nil {
			return errors.Wrap(err, "创建系统配置失败")
		}
		return nil
	}); err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"SysConfigLogic.Create 创建系统配置[%s]失败", req.UUID).ToBizResult()
	}

	_ = l.RenewByUUID(req.UUID)
	return types.NewBizResult(codes.AddSuccess).
		SetI18nMessage(i18n.MsgKeyAddSuccess)
}

// Update 编辑系统常量配置，并刷新对应缓存。
func (l *SysConfigLogic) Update(req *types.SaveSysConfigReq) *types.BizResult {
	var old model.SysConfig
	expectedVersion := -1
	if req.Version != nil {
		expectedVersion = *req.Version
	}
	if err := l.svc.WriteDB(svc.DatabaseMain).Where("id = ?", req.ID).First(&old).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.NotFound(i18n.MsgKeyConfigNotFound, err,
				"SysConfigLogic.Update 配置ID[%d]不存在", req.ID).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"SysConfigLogic.Update 查询配置ID[%d]失败", req.ID).ToBizResult()
	}
	if old.Type != req.Type {
		err := errors.Errorf("配置类型不允许修改")
		return types.NewBizResult(codes.ParamError).
			SetI18nMessage(i18n.MsgKeyParamErrorFormat, err.Error()).
			WithError(wrapLogicError(err, "SysConfigLogic.Update 参数校验失败"))
	}

	valueRaw, err := req.ValueRawMessage()
	if err != nil {
		return types.NewBizResult(codes.ParamError).
			SetI18nMessage(i18n.MsgKeyParamErrorFormat, err.Error()).
			WithError(wrapLogicError(err, "SysConfigLogic.Update 参数校验失败"))
	}
	exampleRaw, err := req.ExampleRawMessage()
	if err != nil {
		return types.NewBizResult(codes.ParamError).
			SetI18nMessage(i18n.MsgKeyParamErrorFormat, err.Error()).
			WithError(wrapLogicError(err, "SysConfigLogic.Update 参数校验失败"))
	}
	value, example, err := normalizeSysConfigJSON(req.Type, valueRaw, exampleRaw)
	if err != nil {
		return types.NewBizResult(codes.ParamError).
			SetI18nMessage(i18n.MsgKeyParamErrorFormat, err.Error()).
			WithError(wrapLogicError(err, "SysConfigLogic.Update 参数校验失败"))
	}
	nextPid := old.Pid
	if req.Pid > 0 || old.Pid == 0 {
		nextPid = req.Pid
	}

	if err = l.svc.WriteDB(svc.DatabaseMain).Transaction(func(tx *gorm.DB) error {
		pids, err := l.sysConfigPidsTx(tx, nextPid, req.ID)
		if err != nil {
			return errors.Tag(err)
		}
		if err := l.ensureSysConfigUUIDUniqueTx(tx, req.UUID, req.ID); err != nil {
			return errors.Tag(err)
		}
		result := tx.Model(&model.SysConfig{}).
			Where("id = ? AND version = ?", req.ID, expectedVersion).
			Updates(map[string]any{
				"uuid":       req.UUID,
				"title":      req.Title,
				"value":      value,
				"example":    example,
				"remark":     req.Remark,
				"page":       req.Page,
				"pid":        nextPid,
				"pids":       pids,
				"version":    gorm.Expr("version + 1"),
				"updated_at": time.Now(),
			})
		if result.Error != nil {
			return errors.Wrap(result.Error, "更新系统配置失败")
		}
		if result.RowsAffected == 0 {
			return errors.Errorf("配置已被其他人修改，请刷新后重试")
		}
		return nil
	}); err != nil {
		if strings.Contains(err.Error(), "配置已被其他人修改，请刷新后重试") {
			return types.NewBizResult(codes.ParamError).
				SetI18nMessage(i18n.MsgKeyParamErrorFormat, "配置已被其他人修改，请刷新后重试").
				WithError(wrapLogicError(err, "SysConfigLogic.Update 乐观锁校验失败"))
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"SysConfigLogic.Update 更新系统配置ID[%d]失败", req.ID).ToBizResult()
	}

	_ = l.RdsDelKeys(tableCachePhysicalAndLegacyKeys(l.BaseLogic, fmt.Sprintf(keys.SysConfigUUID, old.UUID))...)
	_ = l.RenewByUUID(req.UUID)
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess)
}

// GetCache 查询指定系统配置的缓存值，缓存缺失时回源重建。
func (l *SysConfigLogic) GetCache(req *types.UUIDPathReq) *types.BizResult {
	value, err := l.GetCachedValue(req.UUID)
	if err != nil {
		rawCache, rawErr := l.getCacheHash(req.UUID)
		if rawErr == nil && len(rawCache) > 0 {
			return types.NewBizResult(codes.Success).
				SetI18nMessage(i18n.MsgKeyQuerySuccess).
				WithData(map[string]any{
					"decodeError": err.Error(),
					"raw":         rawCache,
					"value":       rawCache["value"],
				})
		}
		return types.ServerError(i18n.MsgKeyCacheInfoFail, err,
			"SysConfigLogic.GetCache 查询配置UUID[%s]缓存失败", req.UUID).ToBizResult()
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(map[string]any{"value": value})
}

// Renew 刷新指定系统配置缓存。
func (l *SysConfigLogic) Renew(req *types.UUIDPathReq) *types.BizResult {
	if err := l.RenewByUUID(req.UUID); err != nil {
		return types.ServerError(i18n.MsgKeyCacheInfoFail, err,
			"SysConfigLogic.Renew 刷新配置UUID[%s]缓存失败", req.UUID).ToBizResult()
	}
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess)
}

// GetCachedValue 读取指定配置缓存并转换为业务类型。
func (l *SysConfigLogic) GetCachedValue(uuid string) (any, error) {
	if l.Redis() == nil {
		return nil, errors.Errorf("Redis未初始化")
	}
	manager, err := tableCacheManager(l.BaseLogic)
	if err != nil {
		return nil, errors.Tag(err)
	}
	var cache map[string]string
	result, err := manager.LoadThrough(l.Context(), tableCachePhysicalKey(l.BaseLogic, fmt.Sprintf(keys.SysConfigUUID, uuid)), &cache, nil)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if result.State == tablecache.LookupStateEmpty || len(cache) == 0 {
		return nil, redis.Nil
	}
	if cacheIsEmptyMarker(cache["value"]) {
		return nil, redis.Nil
	}
	typ, _ := strconv.Atoi(cache["type"])
	return decodeSysConfigValue(typ, cache["value"])
}

// getCacheHash 读取指定系统配置缓存原始 Hash 数据。
func (l *SysConfigLogic) getCacheHash(uuid string) (map[string]string, error) {
	if l.Redis() == nil {
		return nil, errors.Errorf("Redis未初始化")
	}
	key := tableCachePhysicalKey(l.BaseLogic, fmt.Sprintf(keys.SysConfigUUID, uuid))
	return l.Redis().HGetAll(l.Context(), key).Result()
}

// RenewByUUID 从数据库回源刷新指定配置缓存。
func (l *SysConfigLogic) RenewByUUID(uuid string) error {
	if l.Redis() == nil {
		return errors.Errorf("Redis未初始化")
	}
	manager, err := tableCacheManager(l.BaseLogic)
	if err != nil {
		return errors.Tag(err)
	}
	return manager.RefreshByKey(l.Context(), tableCachePhysicalKey(l.BaseLogic, fmt.Sprintf(keys.SysConfigUUID, uuid)))
}

// sysConfigModelToItem 把系统配置模型转换成接口响应项。
func sysConfigModelToItem(cfg model.SysConfig) types.SysConfigItem {
	return types.SysConfigItem{
		ID:        cfg.ID,
		UUID:      cfg.UUID,
		Title:     cfg.Title,
		Type:      cfg.Type,
		Value:     safeRawJSON(cfg.Value),
		Example:   safeRawJSON(cfg.Example),
		Remark:    cfg.Remark,
		Page:      cfg.Page,
		Pid:       cfg.Pid,
		Pids:      cfg.Pids,
		Version:   cfg.Version,
		Editable:  1,
		CreatedAt: formatDateTime(cfg.CreatedAt),
		UpdatedAt: formatDateTime(cfg.UpdatedAt),
	}
}

// safeRawJSON 把数据库 JSON 字符串转换成 json.RawMessage，异常时回落为 JSON 字符串。
func safeRawJSON(value string) json.RawMessage {
	value = strings.TrimSpace(value)
	if value == "" {
		return json.RawMessage("null")
	}
	if json.Valid([]byte(value)) {
		return json.RawMessage(value)
	}
	body, _ := json.Marshal(value)
	return body
}

// normalizeSysConfigJSON 根据配置类型校验并归一化 value/example。
func normalizeSysConfigJSON(typ int, valueRaw json.RawMessage, exampleRaw json.RawMessage) (string, string, error) {
	value, err := normalizeSysConfigValue(typ, valueRaw)
	if err != nil {
		return "", "", errors.Tag(err)
	}
	if len(exampleRaw) == 0 {
		return value, value, nil
	}
	example, err := normalizeSysConfigValue(typ, exampleRaw)
	if err != nil {
		return "", "", errors.Tag(err)
	}
	return value, example, nil
}

// normalizeSysConfigValue 根据配置类型校验单个 JSON 值。
func normalizeSysConfigValue(typ int, raw json.RawMessage) (string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", errors.Errorf("配置值不能为空")
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", errors.Wrap(err, "配置值不是合法JSON")
	}

	switch typ {
	case 0:
		return compactJSON(raw)
	case 1:
		if _, ok := value.(map[string]any); !ok {
			return "", errors.Errorf("配置值不是Object类型")
		}
		return compactJSON(raw)
	case 2:
		if _, ok := value.([]any); !ok {
			return "", errors.Errorf("配置值不是Array类型")
		}
		return compactJSON(raw)
	case 3:
		if _, ok := value.(string); !ok {
			return "", errors.Errorf("配置值不是String类型")
		}
		return compactJSON(raw)
	case 4:
		number, err := jsonNumberToInt(value)
		if err != nil {
			return "", errors.Tag(err)
		}
		return strconv.Itoa(number), nil
	case 5:
		number, err := jsonNumberToFloat(value)
		if err != nil {
			return "", errors.Tag(err)
		}
		return strconv.FormatFloat(number, 'f', -1, 64), nil
	case 6:
		flag, err := jsonValueToBool(value)
		if err != nil {
			return "", errors.Tag(err)
		}
		if flag {
			return "1", nil
		}
		return "0", nil
	default:
		return "", errors.Errorf("配置类型不合法")
	}
}

// compactJSON 压缩 JSON 字符串，减少缓存和数据库里的无意义空白。
func compactJSON(raw json.RawMessage) (string, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return "", errors.Tag(err)
	}
	return buf.String(), nil
}

// jsonNumberToInt 把 JSON 数字或数字字符串转换为 int。
func jsonNumberToInt(value any) (int, error) {
	switch v := value.(type) {
	case json.Number:
		i, err := v.Int64()
		return int(i), errors.Tag(err)
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0, errors.Errorf("配置值不是Integer类型")
		}
		return i, nil
	default:
		return 0, errors.Errorf("配置值不是Integer类型")
	}
}

// jsonNumberToFloat 把 JSON 数字或数字字符串转换为 float64。
func jsonNumberToFloat(value any) (float64, error) {
	switch v := value.(type) {
	case json.Number:
		return v.Float64()
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, errors.Errorf("配置值不是Float类型")
		}
		return f, nil
	default:
		return 0, errors.Errorf("配置值不是Float类型")
	}
}

// jsonValueToBool 把 JSON bool、0/1 或字符串布尔值转换为 bool。
func jsonValueToBool(value any) (bool, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return false, errors.Errorf("配置值不是Boolean类型")
		}
		if i == 0 {
			return false, nil
		}
		if i == 1 {
			return true, nil
		}
		return false, errors.Errorf("配置值不是Boolean类型")
	case string:
		switch strings.ToUpper(strings.TrimSpace(v)) {
		case "1", "TRUE":
			return true, nil
		case "0", "FALSE":
			return false, nil
		default:
			return false, errors.Errorf("配置值不是Boolean类型")
		}
	default:
		return false, errors.Errorf("配置值不是Boolean类型")
	}
}

// decodeSysConfigValue 把缓存中的字符串值还原为业务类型。
func decodeSysConfigValue(typ int, raw string) (any, error) {
	switch typ {
	case 0: // Group (分组)
		return nil, nil
	case 1, 2: // Object, Array
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return nil, errors.Tag(err)
		}
		return value, nil
	case 3: // String
		var value string
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return raw, nil
		}
		return value, nil
	case 4: // Integer
		value, err := strconv.Atoi(raw)
		if err != nil {
			return 0, errors.Tag(err)
		}
		return value, nil
	case 5: // Float
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return 0, errors.Tag(err)
		}
		return value, nil
	case 6: // Boolean
		return raw == "1" || strings.EqualFold(raw, "true"), nil
	default:
		return raw, nil
	}
}

// sysConfigPidsTx 在事务内计算配置族谱。
func (l *SysConfigLogic) sysConfigPidsTx(tx *gorm.DB, pid int, selfID int) (string, error) {
	if pid <= 0 {
		return "", nil
	}
	if pid == selfID {
		return "", errors.Errorf("上级配置不能是自己")
	}
	var parent model.SysConfig
	if err := tx.Where("id = ?", pid).First(&parent).Error; err != nil {
		return "", errors.Wrap(err, "上级配置不存在")
	}
	if containsTreeID(parent.Pids, selfID) {
		return "", errors.Errorf("不能把配置移动到自己的子级下面")
	}
	return buildTreePids(parent.ID, parent.Pids), nil
}

// ensureSysConfigUUIDUniqueTx 校验系统配置 UUID 唯一。
func (l *SysConfigLogic) ensureSysConfigUUIDUniqueTx(tx *gorm.DB, uuid string, ignoreID int) error {
	var count int64
	query := tx.Model(&model.SysConfig{}).Where("uuid = ?", strings.TrimSpace(uuid))
	if ignoreID > 0 {
		query = query.Where("id <> ?", ignoreID)
	}
	if err := query.Count(&count).Error; err != nil {
		return errors.Wrap(err, "检查配置UUID唯一失败")
	}
	if count > 0 {
		return errors.Errorf("配置UUID[%s]已存在", uuid)
	}
	return nil
}
