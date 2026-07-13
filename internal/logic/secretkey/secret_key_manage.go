package secretkey

import (
	corelogic "admin/internal/logic"
	securitylogic "admin/internal/logic/security"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net/http"
	"strings"
	"time"

	"admin/common/codes"
	i18n "admin/common/i18n"
	"admin/internal/infra/loggerx"
	"admin/internal/model"
	"admin/internal/security"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
)

const (
	// secretKeyRSASignCheckPayload 是后台秘钥自检时统一使用的 RSA 待签名字符串，便于日志和自检结果对齐。
	secretKeyRSASignCheckPayload = "admin-sign-check"
)

// NewSecretKeyManageLogic 创建秘钥管理业务逻辑对象。
func NewSecretKeyManageLogic(r *http.Request, svcCtx *svc.ServiceContext) *SecretKeyLogic {
	return &SecretKeyLogic{
		BaseLogic: corelogic.NewBaseLogic(r, svcCtx),
	}
}

// logSecretKeySignCheckFailure 输出签名自检失败所需的排障材料。
func (l *SecretKeyLogic) logSecretKeySignCheckFailure(uuid string, keyVersion string, stage string, signPayload string, signValue string, secretType string, secretValue string, err error) {
	// 自检日志统一复用当前请求上下文字段，保证 trace、审计和后台操作日志能串到同一条链路。
	fields := []logx.LogField{
		logx.Field("uuid", strings.TrimSpace(uuid)),
		logx.Field("key_version", strings.TrimSpace(keyVersion)),
		logx.Field("stage", strings.TrimSpace(stage)),
		logx.Field("sign_payload", signPayload),
		logx.Field("sign_value", signValue),
		logx.Field("secret_type", strings.TrimSpace(secretType)),
		logx.Field("secret_value", secretValue),
	}
	if err != nil {
		fields = append(fields, loggerx.ErrorFields(errors.Tag(err))...)
		loggerx.Infow(l.Ctx, "秘钥 签名自检失败", fields...)
		return
	}
	fields = append(fields, loggerx.ErrorTextFields("secret_key.signature.self_check.failed")...)
	loggerx.Infow(l.Ctx, "秘钥 签名自检失败", fields...)
}

// List 分页查询秘钥配置列表。
func (l *SecretKeyLogic) List(req *types.SecretKeyListReq) *types.BizResult {
	readDB := l.Svc.ReadDB(svc.DatabaseMain)
	dbq := readDB.Model(&model.SecretKey{})
	if req.UUID != "" {
		dbq = dbq.Where("uuid = ?", req.UUID)
	}
	if req.Title != "" {
		dbq = dbq.Where("title LIKE ?", "%"+req.Title+"%")
	}
	if req.Status != nil {
		dbq = dbq.Where("status = ?", *req.Status)
	}
	if req.SignStatus != nil {
		dbq = dbq.Where("sign_status = ?", *req.SignStatus)
	}
	if req.CryptoStatus != nil {
		dbq = dbq.Where("crypto_status = ?", *req.CryptoStatus)
	}
	if req.StableVersion != "" {
		dbq = dbq.Where("stable_version = ?", req.StableVersion)
	}

	orderBy := corelogic.NormalizedOrderField(req.OrderBy, "id")
	list, total, err := model.List[model.SecretKey](dbq, req.GetPageReq.Page, req.PageSize, orderBy, corelogic.NormalizedOrderDirection(req.Order))
	if err != nil {
		return types.DBError(i18n.MsgKeyDBErrorFormat, err,
			"SecretKeyLogic.List 查询秘钥列表失败").ToBizResult()
	}
	versionMap, err := l.listSecretKeyVersionsByRows(list)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBErrorFormat, err,
			"SecretKeyLogic.List 查询秘钥版本失败").ToBizResult()
	}

	items := make([]types.SecretKeyItem, 0, len(list))
	for _, row := range list {
		versions := versionMap[row.UUID]
		selected := selectSecretKeyVersion(versions, row.StableVersion, row.StableVersion)
		items = append(items, secretKeyModelToItem(row, versions, selected, true))
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(types.ListResp[types.SecretKeyItem]{List: items, Total: total})
}

// Get 查询单个秘钥详情，编辑场景返回真实路径，并要求当前操作人完成 MFA 二次确认。
func (l *SecretKeyLogic) Get(req *types.SecretKeyDetailReq) *types.BizResult {
	if req.ID <= 0 {
		err := errors.Errorf("秘钥ID不能为空")
		return types.ParamErrorResult(err).
			WithError(corelogic.WrapLogicError(err, "SecretKeyLogic.Get 参数校验失败"))
	}
	if err := l.requireSecretKeyMFATwoStep(req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.secretKeyMFABizResult(err)
	}
	row, versions, err := l.getSecretKeyByID(req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.NotFound(i18n.MsgKeyConfigNotFound, err,
				"SecretKeyLogic.Get 秘钥ID[%d]不存在", req.ID).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBErrorFormat, err,
			"SecretKeyLogic.Get 查询秘钥ID[%d]失败", req.ID).ToBizResult()
	}
	selected := selectSecretKeyVersion(versions, req.KeyVersion, row.StableVersion)
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(secretKeyModelToItem(row, versions, selected, false))
}

// Create 新增秘钥主配置和首个版本，并刷新运行时缓存。
func (l *SecretKeyLogic) Create(req *types.SaveSecretKeyReq) *types.BizResult {
	if err := l.requireSecretKeyMFATwoStep(req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.secretKeyMFABizResult(err)
	}
	if err := validateSecretKeySaveReq(req, nil); err != nil {
		return types.ParamErrorResult(err).
			WithError(corelogic.WrapLogicError(err, "SecretKeyLogic.Create 参数校验失败"))
	}
	now := time.Now()
	mainRow := model.SecretKey{
		UUID:          req.UUID,
		Title:         req.Title,
		StableVersion: req.StableVersion,
		GrayVersion:   normalizedGrayVersion(req),
		GrayPercent:   normalizedGrayPercent(req),
		GraySalt:      buildSecretKeyGraySalt(normalizedGrayVersion(req)),
		Status:        req.Status,
		SignStatus:    req.SignStatus,
		CryptoStatus:  req.CryptoStatus,
		Remark:        req.Remark,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	versionRow := model.SecretKeyVersion{
		UUID:                   req.UUID,
		KeyVersion:             req.KeyVersion,
		AESKeyRef:              req.AESKeyRef,
		AESIVRef:               req.AESIVRef,
		RSAPublicKeyUserRef:    req.RSAPublicKeyUserRef,
		RSAPublicKeyServerRef:  req.RSAPublicKeyServerRef,
		RSAPrivateKeyServerRef: req.RSAPrivateKeyServerRef,
		Status:                 req.VersionStatus,
		Remark:                 req.Remark,
		CreatedAt:              now,
		UpdatedAt:              now,
	}

	if err := l.Svc.WriteDB(svc.DatabaseMain).Transaction(func(tx *gorm.DB) error {
		if err := l.ensureSecretKeyUUIDUniqueTx(tx, req.UUID, 0); err != nil {
			return errors.Tag(err)
		}
		if err := tx.Create(&mainRow).Error; err != nil {
			return errors.Wrap(err, "创建秘钥主配置失败")
		}
		versionRow.SecretKeyID = mainRow.ID
		if err := tx.Create(&versionRow).Error; err != nil {
			return errors.Wrap(err, "创建秘钥版本失败")
		}
		return nil
	}); err != nil {
		return types.DBError(i18n.MsgKeyDBErrorFormat, err,
			"SecretKeyLogic.Create 创建秘钥[%s]失败", req.UUID).ToBizResult()
	}

	if err := l.RenewSecretKeyCache(req.UUID); err != nil {
		return corelogic.CacheSyncPendingResult(l.Logger, codes.AddSuccess, i18n.MsgKeyCacheSyncPending, err,
			"SecretKeyLogic.Create AppID[%s]缓存同步失败", req.UUID)
	}
	return types.NewBizResult(codes.AddSuccess).
		SetI18nMessage(i18n.MsgKeyAddSuccess)
}

// Update 编辑秘钥主配置或版本配置，并刷新对应运行时缓存。
func (l *SecretKeyLogic) Update(req *types.SaveSecretKeyReq) *types.BizResult {
	if req.ID <= 0 {
		err := errors.Errorf("秘钥ID不能为空")
		return types.ParamErrorResult(err).
			WithError(corelogic.WrapLogicError(err, "SecretKeyLogic.Update 参数校验失败"))
	}
	if err := l.requireSecretKeyMFATwoStep(req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.secretKeyMFABizResult(err)
	}
	oldRow, oldVersions, err := l.getSecretKeyByID(req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.NotFound(i18n.MsgKeyConfigNotFound, err,
				"SecretKeyLogic.Update 秘钥ID[%d]不存在", req.ID).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBErrorFormat, err,
			"SecretKeyLogic.Update 查询秘钥ID[%d]失败", req.ID).ToBizResult()
	}
	if strings.TrimSpace(req.UUID) != strings.TrimSpace(oldRow.UUID) {
		err := errors.Errorf("编辑模式不允许修改秘钥标识")
		return types.ParamErrorResult(err).
			WithError(corelogic.WrapLogicError(err, "SecretKeyLogic.Update 参数校验失败"))
	}
	if err := validateSecretKeySaveReq(req, oldVersions); err != nil {
		return types.ParamErrorResult(err).
			WithError(corelogic.WrapLogicError(err, "SecretKeyLogic.Update 参数校验失败"))
	}

	if err := l.Svc.WriteDB(svc.DatabaseMain).Transaction(func(tx *gorm.DB) error {
		updateMap := map[string]any{
			"title":          req.Title,
			"stable_version": req.StableVersion,
			"gray_version":   normalizedGrayVersion(req),
			"gray_percent":   normalizedGrayPercent(req),
			"status":         req.Status,
			"sign_status":    req.SignStatus,
			"crypto_status":  req.CryptoStatus,
			"remark":         req.Remark,
			"updated_at":     time.Now(),
		}
		if normalizedGrayVersion(req) == "" {
			updateMap["gray_salt"] = ""
		} else if shouldRefreshGraySalt(oldRow, req) {
			updateMap["gray_salt"] = buildSecretKeyGraySalt(normalizedGrayVersion(req))
		}
		if err := tx.Model(&model.SecretKey{}).Where("id = ?", req.ID).Updates(updateMap).Error; err != nil {
			return errors.Wrap(err, "更新秘钥主配置失败")
		}

		var versionRow model.SecretKeyVersion
		err := tx.Where("secret_key_id = ? AND key_version = ?", req.ID, req.KeyVersion).First(&versionRow).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.Wrap(err, "查询秘钥版本失败")
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			versionRow = model.SecretKeyVersion{
				SecretKeyID:            req.ID,
				UUID:                   req.UUID,
				KeyVersion:             req.KeyVersion,
				AESKeyRef:              req.AESKeyRef,
				AESIVRef:               req.AESIVRef,
				RSAPublicKeyUserRef:    req.RSAPublicKeyUserRef,
				RSAPublicKeyServerRef:  req.RSAPublicKeyServerRef,
				RSAPrivateKeyServerRef: req.RSAPrivateKeyServerRef,
				Status:                 req.VersionStatus,
				Remark:                 req.Remark,
				CreatedAt:              time.Now(),
				UpdatedAt:              time.Now(),
			}
			if err := tx.Create(&versionRow).Error; err != nil {
				return errors.Wrap(err, "创建秘钥版本失败")
			}
			return nil
		}
		if err := tx.Model(&model.SecretKeyVersion{}).Where("id = ?", versionRow.ID).Updates(map[string]any{
			"aes_key_ref":                req.AESKeyRef,
			"aes_iv_ref":                 req.AESIVRef,
			"rsa_public_key_user_ref":    req.RSAPublicKeyUserRef,
			"rsa_public_key_server_ref":  req.RSAPublicKeyServerRef,
			"rsa_private_key_server_ref": req.RSAPrivateKeyServerRef,
			"status":                     req.VersionStatus,
			"remark":                     req.Remark,
			"updated_at":                 time.Now(),
		}).Error; err != nil {
			return errors.Wrap(err, "更新秘钥版本失败")
		}
		return nil
	}); err != nil {
		return types.DBError(i18n.MsgKeyDBErrorFormat, err,
			"SecretKeyLogic.Update 更新秘钥ID[%d]失败", req.ID).ToBizResult()
	}

	if err := l.RenewSecretKeyCache(oldRow.UUID); err != nil {
		return corelogic.CacheSyncPendingResult(l.Logger, codes.UpdateSuccess, i18n.MsgKeyCacheSyncPending, err,
			"SecretKeyLogic.Update AppID[%s]缓存同步失败", oldRow.UUID)
	}
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess)
}

// UpdateStatus 修改秘钥状态，并同步刷新运行时缓存。
func (l *SecretKeyLogic) UpdateStatus(req *types.SecretKeyStatusReq) *types.BizResult {
	if err := l.requireSecretKeyMFATwoStep(req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.secretKeyMFABizResult(err)
	}
	status := req.StatusValue()
	row, versions, err := l.getSecretKeyByID(req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.NotFound(i18n.MsgKeyConfigNotFound, err,
				"SecretKeyLogic.UpdateStatus 秘钥ID[%d]不存在", req.ID).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBErrorFormat, err,
			"SecretKeyLogic.UpdateStatus 查询秘钥ID[%d]失败", req.ID).ToBizResult()
	}
	if status == 1 {
		if err := validateSecretKeyRouteWithVersions(&types.SaveSecretKeyReq{
			UUID:          row.UUID,
			Title:         row.Title,
			Status:        status,
			SignStatus:    row.SignStatus,
			CryptoStatus:  row.CryptoStatus,
			StableVersion: row.StableVersion,
			GrayVersion:   row.GrayVersion,
			GrayPercent:   row.GrayPercent,
		}, versions); err != nil {
			return types.ParamErrorResult(err).
				WithError(corelogic.WrapLogicError(err, "SecretKeyLogic.UpdateStatus 参数校验失败"))
		}
		if err := l.validateSecretKeyRequiredVersions(row, versions); err != nil {
			return types.ParamErrorResult(err).
				WithError(corelogic.WrapLogicError(err, "SecretKeyLogic.UpdateStatus 参数校验失败"))
		}
	}
	writeDB := l.Svc.WriteDB(svc.DatabaseMain)
	if err := writeDB.Model(&model.SecretKey{}).Where("id = ?", req.ID).Updates(map[string]any{
		"status":     status,
		"updated_at": time.Now(),
	}).Error; err != nil {
		return types.DBError(i18n.MsgKeyDBErrorFormat, err,
			"SecretKeyLogic.UpdateStatus 更新秘钥ID[%d]状态失败", req.ID).ToBizResult()
	}
	if err := l.RenewSecretKeyCache(row.UUID); err != nil {
		return corelogic.CacheSyncPendingResult(l.Logger, codes.UpdateSuccess, i18n.MsgKeyCacheSyncPending, err,
			"SecretKeyLogic.UpdateStatus AppID[%s]缓存同步失败", row.UUID)
	}
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess)
}

// Renew 刷新指定 AppID 的版本路由和材料缓存。
func (l *SecretKeyLogic) Renew(req *types.SecretKeyRenewReq) *types.BizResult {
	if err := l.requireSecretKeyMFATwoStep(req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.secretKeyMFABizResult(err)
	}
	if err := l.RenewSecretKeyCache(req.UUID); err != nil {
		return types.ServerError(i18n.MsgKeyCacheInfoFail, err,
			"SecretKeyLogic.Renew 刷新秘钥UUID[%s]缓存失败", req.UUID).ToBizResult()
	}
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess)
}

// ValidatePaths 对待保存的秘钥路径执行静态预检，便于前端在保存前发现路径、长度与 PEM 格式问题。
func (l *SecretKeyLogic) ValidatePaths(req *types.SecretKeyValidateReq) *types.BizResult {
	if req == nil {
		return types.ParamErrorResult(errors.Errorf("秘钥预检请求不能为空"))
	}
	if err := req.Validate(); err != nil {
		return types.ParamErrorResult(err)
	}
	if err := l.requireSecretKeyMFATwoStep(req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.secretKeyMFABizResult(err)
	}
	result := l.checkSecretKeyPayload(req.ToSaveSecretKeyReq(), nil, false, false)
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(result)
}

// SelfCheck 对已落库的秘钥执行缓存刷新与签名加解密链路自检，确保实际运行链路可用。
func (l *SecretKeyLogic) SelfCheck(req *types.SecretKeySelfCheckReq) *types.BizResult {
	if req == nil {
		return types.ParamErrorResult(errors.Errorf("秘钥自检请求不能为空"))
	}
	if err := req.Validate(); err != nil {
		return types.ParamErrorResult(err)
	}
	if err := l.requireSecretKeyMFATwoStep(req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.secretKeyMFABizResult(err)
	}
	row, versions, err := l.getSecretKeyByUUID(req.UUID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.NotFound(i18n.MsgKeyConfigNotFound, err,
				"SecretKeyLogic.SelfCheck 秘钥UUID[%s]不存在", req.UUID).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBErrorFormat, err,
			"SecretKeyLogic.SelfCheck 查询秘钥UUID[%s]失败", req.UUID).ToBizResult()
	}
	selected := selectSecretKeyVersion(versions, req.KeyVersion, row.StableVersion)
	if selected == nil {
		return types.ParamErrorResult(errors.Errorf("待自检的秘钥版本不存在"))
	}
	saveReq := buildSaveSecretKeyReq(row, *selected)
	result := l.checkSecretKeyPayload(saveReq, versions, true, true)
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(result)
}

// ensureSecretKeyUUIDUniqueTx 校验秘钥 UUID 唯一。
func (l *SecretKeyLogic) ensureSecretKeyUUIDUniqueTx(tx *gorm.DB, uuid string, ignoreID int) error {
	var count int64
	query := tx.Model(&model.SecretKey{}).Where("uuid = ?", strings.TrimSpace(uuid))
	if ignoreID > 0 {
		query = query.Where("id <> ?", ignoreID)
	}
	if err := query.Count(&count).Error; err != nil {
		return errors.Wrap(err, "检查秘钥UUID唯一失败")
	}
	if count > 0 {
		return errors.Errorf("秘钥UUID[%s]已存在", uuid)
	}
	return nil
}

// requireSecretKeyMFATwoStep 校验秘钥管理敏感操作的 MFA 二次票据。
func (l *SecretKeyLogic) requireSecretKeyMFATwoStep(twoStepKey string, twoStepValue string) error {
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin == nil || ctxAdmin.ID <= 0 {
		return types.Nil
	}
	securityLogic := securitylogic.NewSecurityLogic(l.Ctx, l.Svc)
	needTwoStep, err := securityLogic.NeedOperateMFATwoStep(securitylogic.MFAScenarioSecretKeyManage)
	if err != nil {
		return errors.Tag(err)
	}
	if !needTwoStep {
		return nil
	}
	return securityLogic.VerifyMFATwoStepTicket(ctxAdmin.ID, securitylogic.MFAScenarioSecretKeyManage, twoStepKey, twoStepValue)
}

// secretKeyMFABizResult 复用管理员敏感操作的 MFA 错误响应格式。
func (l *SecretKeyLogic) secretKeyMFABizResult(err error) *types.BizResult {
	return securitylogic.OperateMFABizResult(err, "SecretKeyLogic.secretKeyMFABizResult")
}

// validateSecretKeyEnabledValues 校验单个启用版本在当前安全开关组合下所需的秘钥材料是否可用。
func validateSecretKeyEnabledValues(req *types.SaveSecretKeyReq) error {
	if req == nil || req.VersionStatus != 1 {
		return nil
	}
	if secretKeyCryptoEnabled(req) {
		key, err := normalizeSecretText(req.AESKeyRef)
		if err != nil {
			return errors.Wrap(err, "AES KEY文件不可用")
		}
		if len(key) != 16 && len(key) != 24 && len(key) != 32 {
			return errors.Errorf("AES KEY长度必须是16、24或32位")
		}
		iv, err := normalizeSecretText(req.AESIVRef)
		if err != nil {
			return errors.Wrap(err, "AES IV文件不可用")
		}
		if len(iv) != 16 {
			return errors.Errorf("AES IV长度必须是16位")
		}
	}
	if secretKeySignEnabled(req) || secretKeyCryptoEnabled(req) {
		if _, err := resolvePEMText(req.RSAPublicKeyUserRef); err != nil {
			return errors.Wrap(err, "用户 RSA公钥文件不可用")
		}
		if _, err := resolvePEMText(req.RSAPrivateKeyServerRef); err != nil {
			return errors.Wrap(err, "服务端 RSA私钥文件不可用")
		}
	}
	if secretKeySignEnabled(req) {
		if strings.TrimSpace(req.RSAPublicKeyServerRef) != "" {
			if _, err := resolvePEMText(req.RSAPublicKeyServerRef); err != nil {
				return errors.Wrap(err, "服务端 RSA公钥文件不可用")
			}
			return nil
		}
		if _, err := deriveServerPublicPEMFromPrivateRef(req.RSAPrivateKeyServerRef); err != nil {
			return errors.Wrap(err, "服务端 RSA公钥派生失败")
		}
	}
	return nil
}

// secretKeySignEnabled 判断当前请求是否启用签名验签链路。
func secretKeySignEnabled(req *types.SaveSecretKeyReq) bool {
	return req != nil && req.VersionStatus == 1 && req.SignStatus == 1
}

// secretKeyCryptoEnabled 判断当前请求是否启用加密解密链路。
func secretKeyCryptoEnabled(req *types.SaveSecretKeyReq) bool {
	return req != nil && req.VersionStatus == 1 && req.CryptoStatus == 1
}

// checkSecretKeyPayload 统一执行秘钥静态校验与运行态自检，供预检、自检、启用前校验复用。
func (l *SecretKeyLogic) checkSecretKeyPayload(req *types.SaveSecretKeyReq, versions []model.SecretKeyVersion, refreshCache bool, runtimeCheck bool) types.SecretKeyCheckResult {
	start := time.Now()
	sanitizedReq := req
	if sanitizedReq == nil {
		sanitizedReq = &types.SaveSecretKeyReq{}
	}
	items := make([]types.SecretKeyCheckItem, 0, 24)
	result := types.SecretKeyCheckResult{
		UUID:           strings.TrimSpace(sanitizedReq.UUID),
		Title:          strings.TrimSpace(sanitizedReq.Title),
		KeyVersion:     strings.TrimSpace(sanitizedReq.KeyVersion),
		Mode:           "validate",
		Status:         sanitizedReq.Status,
		CanSave:        true,
		CanEnable:      true,
		RuntimeChecked: runtimeCheck,
		CacheRefreshed: false,
	}
	if runtimeCheck {
		result.Mode = "self_check"
	}
	signEnabled := secretKeySignEnabled(sanitizedReq)
	cryptoEnabled := secretKeyCryptoEnabled(sanitizedReq)

	appendItem := func(key string, label string, passed bool, successMessage string, failMessage string) {
		message := successMessage
		level := "success"
		if !passed {
			message = failMessage
			level = "error"
		}
		items = append(items, types.SecretKeyCheckItem{
			Key:     key,
			Label:   label,
			Passed:  passed,
			Level:   level,
			Message: message,
		})
		if !passed {
			result.CanEnable = false
		}
	}
	appendError := func(key string, label string, safeMessage string) {
		errText := strings.TrimSpace(safeMessage)
		if errText == "" {
			errText = "校验失败，请检查配置"
		}
		items = append(items, types.SecretKeyCheckItem{
			Key:     key,
			Label:   label,
			Passed:  false,
			Level:   "error",
			Message: errText,
		})
		result.CanSave = false
		result.CanEnable = false
	}

	if result.UUID == "" {
		appendError("uuid", "秘钥标识", "秘钥标识不能为空")
	}
	if strings.TrimSpace(sanitizedReq.Title) == "" {
		appendError("title", "秘钥标题", "秘钥标题不能为空")
	}
	if result.KeyVersion == "" {
		appendError("key_version", "秘钥版本", "秘钥版本不能为空")
	}

	if err := validateSecretKeyRouteWithVersions(sanitizedReq, versions); err != nil {
		appendError("route.version", "版本路由配置", err.Error())
	} else {
		appendItem("route.version", "版本路由配置", true, "稳定版本与灰度版本配置合法", "")
	}

	aesKeyText := ""
	aesIVText := ""
	userPublicPEM := ""
	serverPublicPEM := ""
	serverPrivatePEM := ""
	var serverPublicErr error
	var serverPrivateErr error
	var serverPublicKey *rsa.PublicKey
	var serverPrivateKey *rsa.PrivateKey

	if cryptoEnabled {
		if _, err := normalizeSecretRef(sanitizedReq.AESKeyRef); err != nil {
			appendError("aes_key_ref.path", "AES KEY路径", "开启加密解密后，AES KEY 必须填写绝对路径，且不能直接录入明文或 PEM")
		} else {
			appendItem("aes_key_ref.path", "AES KEY路径", true, "AES KEY 路径格式正确", "")
		}
		if _, err := normalizeSecretRef(sanitizedReq.AESIVRef); err != nil {
			appendError("aes_iv_ref.path", "AES IV路径", "开启加密解密后，AES IV 必须填写绝对路径，且不能直接录入明文")
		} else {
			appendItem("aes_iv_ref.path", "AES IV路径", true, "AES IV 路径格式正确", "")
		}
		var err error
		aesKeyText, err = normalizeSecretText(sanitizedReq.AESKeyRef)
		if err != nil {
			appendError("aes_key_ref.file", "AES KEY文件", "AES KEY 文件不存在、不可读或内容为空")
		} else {
			appendItem("aes_key_ref.file", "AES KEY文件", true, "AES KEY 文件可读取", "")
		}
		aesIVText, err = normalizeSecretText(sanitizedReq.AESIVRef)
		if err != nil {
			appendError("aes_iv_ref.file", "AES IV文件", "AES IV 文件不存在、不可读或内容为空")
		} else {
			appendItem("aes_iv_ref.file", "AES IV文件", true, "AES IV 文件可读取", "")
		}
		aesKeyLengthPassed := len(aesKeyText) == 16 || len(aesKeyText) == 24 || len(aesKeyText) == 32
		appendItem("aes_key_ref.length", "AES KEY长度", aesKeyLengthPassed, "AES KEY 长度合法", "AES KEY长度必须是16、24或32位")
		aesIVLengthPassed := len(aesIVText) == 16
		appendItem("aes_iv_ref.length", "AES IV长度", aesIVLengthPassed, "AES IV 长度合法", "AES IV长度必须是16位")
	} else {
		appendItem("crypto_status", "加密解密状态", true, "当前已关闭加密解密链路，跳过 AES 校验", "")
	}

	if signEnabled || cryptoEnabled {
		if _, err := normalizeSecretRef(sanitizedReq.RSAPublicKeyUserRef); err != nil {
			appendError("rsa_public_key_user_ref.path", "用户 RSA公钥路径", "启用签名验签或加密解密后，用户 RSA 公钥必须填写绝对路径，且不能直接录入 PEM")
		} else {
			appendItem("rsa_public_key_user_ref.path", "用户 RSA公钥路径", true, "用户 RSA 公钥路径格式正确", "")
		}
		if _, err := normalizeSecretRef(sanitizedReq.RSAPrivateKeyServerRef); err != nil {
			appendError("rsa_private_key_server_ref.path", "服务端 RSA私钥路径", "启用签名验签或加密解密后，服务端 RSA 私钥必须填写绝对路径，且不能直接录入 PEM")
		} else {
			appendItem("rsa_private_key_server_ref.path", "服务端 RSA 私钥路径", true, "服务端 RSA 私钥路径格式正确", "")
		}
		var err error
		userPublicPEM, err = resolvePEMText(sanitizedReq.RSAPublicKeyUserRef)
		if err != nil {
			appendError("rsa_public_key_user_ref.file", "用户 RSA公钥文件", "用户 RSA 公钥文件不存在、不可读或内容不是有效 PEM")
		} else {
			appendItem("rsa_public_key_user_ref.file", "用户 RSA公钥文件", true, "用户 RSA 公钥文件可读取", "")
		}
		serverPrivatePEM, err = resolvePEMText(sanitizedReq.RSAPrivateKeyServerRef)
		if err != nil {
			appendError("rsa_private_key_server_ref.file", "服务端 RSA私钥文件", "服务端 RSA 私钥文件不存在、不可读或内容不是有效 PEM")
		} else {
			appendItem("rsa_private_key_server_ref.file", "服务端 RSA私钥文件", true, "服务端 RSA 私钥文件可读取", "")
		}
		if _, userPublicErr := security.ParseRSAPublicKey(userPublicPEM); userPublicErr != nil {
			appendError("rsa_public_key_user_ref.pem", "用户 RSA公钥格式", "用户 RSA 公钥 PEM 格式不合法")
		} else {
			appendItem("rsa_public_key_user_ref.pem", "用户 RSA公钥格式", true, "用户 RSA 公钥 PEM 格式正确", "")
		}
		serverPrivateKey, serverPrivateErr = security.ParseRSAPrivateKey(serverPrivatePEM)
		if serverPrivateErr != nil {
			appendError("rsa_private_key_server_ref.pem", "服务端 RSA私钥格式", "服务端 RSA 私钥 PEM 格式不合法")
		} else {
			appendItem("rsa_private_key_server_ref.pem", "服务端 RSA私钥格式", true, "服务端 RSA 私钥 PEM 格式正确", "")
		}
	} else {
		appendItem("sign_status", "签名验签状态", true, "当前已关闭签名验签链路，跳过 RSA 材料校验", "")
	}

	if signEnabled {
		if strings.TrimSpace(sanitizedReq.RSAPublicKeyServerRef) == "" {
			if serverPrivateErr != nil || serverPrivateKey == nil {
				appendError("rsa_public_key_server_ref.derived", "服务端 RSA公钥", "服务端 RSA 私钥格式未通过，无法派生公钥")
			} else {
				var err error
				serverPublicPEM, err = deriveRSAPublicPEMFromPrivateKey(serverPrivateKey)
				if err != nil {
					appendError("rsa_public_key_server_ref.derived", "服务端 RSA公钥", "服务端 RSA 公钥派生失败")
				} else {
					serverPublicKey = &serverPrivateKey.PublicKey
					appendItem("rsa_public_key_server_ref.derived", "服务端 RSA公钥", true, "未配置公钥路径，已由服务端私钥派生", "")
				}
			}
		} else {
			if _, err := normalizeSecretRef(sanitizedReq.RSAPublicKeyServerRef); err != nil {
				appendError("rsa_public_key_server_ref.path", "服务端 RSA公钥路径", "服务端 RSA 公钥路径格式错误，不能直接录入 PEM")
			} else {
				appendItem("rsa_public_key_server_ref.path", "服务端 RSA 公钥路径", true, "服务端 RSA 公钥路径格式正确", "")
			}
			var err error
			serverPublicPEM, err = resolvePEMText(sanitizedReq.RSAPublicKeyServerRef)
			if err != nil {
				appendError("rsa_public_key_server_ref.file", "服务端 RSA公钥文件", "服务端 RSA 公钥文件不存在、不可读或内容不是有效 PEM")
			} else {
				appendItem("rsa_public_key_server_ref.file", "服务端 RSA公钥文件", true, "服务端 RSA 公钥文件可读取", "")
			}
			serverPublicKey, serverPublicErr = security.ParseRSAPublicKey(serverPublicPEM)
			if serverPublicErr != nil {
				appendError("rsa_public_key_server_ref.pem", "服务端 RSA公钥格式", "服务端 RSA 公钥 PEM 格式不合法")
			} else {
				appendItem("rsa_public_key_server_ref.pem", "服务端 RSA公钥格式", true, "服务端 RSA 公钥 PEM 格式正确", "")
			}
		}
		if serverPublicKey != nil && serverPublicErr == nil && serverPrivateErr == nil {
			rsaPairPassed := serverPublicKey.N.Cmp(serverPrivateKey.N) == 0 && serverPublicKey.E == serverPrivateKey.E
			appendItem("rsa_server_pair.match", "服务端 RSA配对", rsaPairPassed, "服务端 RSA 公私钥配对正确", "服务端 RSA 公钥与私钥不是同一对")
		} else {
			appendError("rsa_server_pair.match", "服务端 RSA配对", "服务端 RSA 公私钥格式未通过，暂时无法判断是否配对")
		}
	}

	if refreshCache && result.UUID != "" {
		if err := l.RenewSecretKeyCache(result.UUID); err != nil {
			appendError("cache.refresh", "缓存刷新", "刷新秘钥缓存失败，请检查 Redis、数据库和当前秘钥配置")
		} else {
			result.CacheRefreshed = true
			appendItem("cache.refresh", "缓存刷新", true, "秘钥缓存刷新成功", "")
		}
	}

	if runtimeCheck && result.UUID != "" {
		if cryptoEnabled {
			if aesCipher, err := security.NewAESCipher(aesKeyText, aesIVText); err != nil {
				appendError("runtime.aes.init", "AES运行态初始化", "AES 运行态初始化失败，请检查 AES KEY 与 IV 内容")
			} else {
				appendItem("runtime.aes.init", "AES运行态初始化", true, "AES 运行态初始化成功", "")
				const aesPlaintext = "admin-secret-check"
				cipherText, encryptErr := aesCipher.Encrypt(aesPlaintext)
				if encryptErr != nil {
					appendError("runtime.aes.encrypt", "AES加密自检", "AES 加密自检失败")
				} else {
					plainText, decryptErr := aesCipher.Decrypt(cipherText)
					if decryptErr != nil {
						appendError("runtime.aes.decrypt", "AES解密自检", "AES 解密自检失败")
					} else {
						appendItem("runtime.aes.decrypt", "AES加解密自检", plainText == aesPlaintext, "AES 加解密链路可用", "AES 解密结果与原文不一致")
					}
				}
			}
		}

		if signEnabled {
			signer, signerErr := security.NewRSASigner(serverPrivatePEM, "")
			if signerErr != nil {
				l.logSecretKeySignCheckFailure(result.UUID, result.KeyVersion, "runtime.rsa.signer", secretKeyRSASignCheckPayload, "", RSAServerPrivateKey, serverPrivatePEM, signerErr)
				appendError("runtime.rsa.signer", "RSA签名器初始化", "RSA 签名器初始化失败，请检查服务端私钥")
			} else {
				appendItem("runtime.rsa.signer", "RSA签名器初始化", true, "RSA 签名器初始化成功", "")
				signValue, signErr := signer.Sign(secretKeyRSASignCheckPayload)
				if signErr != nil {
					l.logSecretKeySignCheckFailure(result.UUID, result.KeyVersion, "runtime.rsa.sign", secretKeyRSASignCheckPayload, "", RSAServerPrivateKey, serverPrivatePEM, signErr)
					appendError("runtime.rsa.sign", "RSA签名自检", "RSA 签名自检失败")
				} else {
					appendItem("runtime.rsa.sign", "RSA签名自检", true, "RSA 签名链路可用", "")
					verifySigner, verifyErr := security.NewRSASigner("", serverPublicPEM)
					if verifyErr != nil {
						l.logSecretKeySignCheckFailure(result.UUID, result.KeyVersion, "runtime.rsa.verify_init", secretKeyRSASignCheckPayload, signValue, RSAServerPublicKey, serverPublicPEM, verifyErr)
						appendError("runtime.rsa.verify_init", "RSA验签器初始化", "RSA 验签器初始化失败，请检查服务端公钥")
					} else {
						verified, verifyRunErr := verifySigner.Verify(secretKeyRSASignCheckPayload, signValue)
						if verifyRunErr != nil {
							l.logSecretKeySignCheckFailure(result.UUID, result.KeyVersion, "runtime.rsa.verify", secretKeyRSASignCheckPayload, signValue, RSAServerPublicKey, serverPublicPEM, verifyRunErr)
							appendError("runtime.rsa.verify", "RSA验签自检", "RSA 验签自检失败")
						} else {
							if !verified {
								l.logSecretKeySignCheckFailure(result.UUID, result.KeyVersion, "runtime.rsa.verify", secretKeyRSASignCheckPayload, signValue, RSAServerPublicKey, serverPublicPEM, errors.New("RSA验签结果不匹配"))
							}
							appendItem("runtime.rsa.verify", "RSA验签自检", verified, "RSA 验签链路可用", "RSA 验签失败，请确认服务端公钥与服务端私钥是否对应")
						}
					}
				}
			}
		}

		if cryptoEnabled {
			rsaCipher, rsaCipherErr := security.NewRSACipher(serverPrivatePEM, userPublicPEM)
			if rsaCipherErr != nil {
				appendError("runtime.rsa.cipher_init", "RSA加解密器初始化", "RSA 加解密器初始化失败，请检查服务端私钥与用户公钥")
			} else {
				appendItem("runtime.rsa.cipher_init", "RSA加解密器初始化", true, "RSA 加解密器初始化成功", "")
				const rsaPlaintext = "admin-rsa-check"
				_, encryptErr := rsaCipher.Encrypt(rsaPlaintext)
				if encryptErr != nil {
					appendError("runtime.rsa.encrypt", "RSA加密自检", "RSA 加密自检失败")
				} else {
					requestDecryptPassed, decryptErr := runSecretKeyRSARequestDecryptSelfCheck(serverPrivatePEM)
					if decryptErr != nil {
						appendError("runtime.rsa.decrypt", "RSA解密自检", "RSA 解密自检失败")
					} else {
						appendItem("runtime.rsa.decrypt", "RSA请求解密自检", requestDecryptPassed, "RSA 请求解密链路可用", "RSA 请求解密失败，请确认服务端公钥与服务端私钥是否对应")
					}
				}
			}
		}
	}

	result.Items = items
	result.AllPassed = true
	for _, item := range items {
		if !item.Passed {
			result.AllPassed = false
			break
		}
	}
	result.CanSave = result.CanSave && len(items) > 0
	if sanitizedReq.Status != 1 {
		result.CanEnable = false
	} else {
		result.CanEnable = result.CanEnable && result.AllPassed
	}
	result.CheckedAt = corelogic.FormatDateTime(time.Now())
	result.DurationMs = time.Since(start).Milliseconds()
	return result
}

// deriveRSAPublicPEMFromPrivateKey 从服务端 RSA 私钥派生对应公钥 PEM。
func deriveRSAPublicPEMFromPrivateKey(privateKey *rsa.PrivateKey) (string, error) {
	if privateKey == nil {
		return "", errors.New("服务端 RSA 私钥未解析")
	}
	publicASN1, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", errors.Wrap(err, "派生服务端 RSA 公钥失败")
	}
	publicPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicASN1,
	})
	if len(publicPEM) == 0 {
		return "", errors.New("派生服务端 RSA 公钥 PEM 失败")
	}
	return string(publicPEM), nil
}

// runSecretKeyRSARequestDecryptSelfCheck 执行“服务端公钥加密 -> 服务端私钥解密”的请求解密链路自检。
func runSecretKeyRSARequestDecryptSelfCheck(serverPrivatePEM string) (bool, error) {
	const rsaPlaintext = "admin-rsa-check"
	serverPrivateKey, err := security.ParseRSAPrivateKey(serverPrivatePEM)
	if err != nil {
		return false, errors.Tag(err)
	}
	serverPublicPEM, err := deriveRSAPublicPEMFromPrivateKey(serverPrivateKey)
	if err != nil {
		return false, errors.Tag(err)
	}
	rsaCipher, err := security.NewRSACipher(serverPrivatePEM, serverPublicPEM)
	if err != nil {
		return false, errors.Tag(err)
	}
	cipherText, err := rsaCipher.Encrypt(rsaPlaintext)
	if err != nil {
		return false, errors.Tag(err)
	}
	plainText, err := rsaCipher.Decrypt(cipherText)
	if err != nil {
		return false, errors.Tag(err)
	}
	return plainText == rsaPlaintext, nil
}

// getSecretKeyByID 查询秘钥主配置和全部版本。
func (l *SecretKeyLogic) getSecretKeyByID(id int) (model.SecretKey, []model.SecretKeyVersion, error) {
	readDB := l.Svc.ReadDB(svc.DatabaseMain)
	var row model.SecretKey
	if err := readDB.Where("id = ?", id).First(&row).Error; err != nil {
		return model.SecretKey{}, nil, errors.Tag(err)
	}
	versions, err := l.listSecretKeyVersionsByUUID(row.UUID)
	if err != nil {
		return model.SecretKey{}, nil, errors.Tag(err)
	}
	return row, versions, nil
}

// getSecretKeyByUUID 查询秘钥主配置和全部版本。
func (l *SecretKeyLogic) getSecretKeyByUUID(uuid string) (model.SecretKey, []model.SecretKeyVersion, error) {
	readDB := l.Svc.ReadDB(svc.DatabaseMain)
	var row model.SecretKey
	if err := readDB.Where("uuid = ?", strings.TrimSpace(uuid)).First(&row).Error; err != nil {
		return model.SecretKey{}, nil, errors.Tag(err)
	}
	versions, err := l.listSecretKeyVersionsByUUID(row.UUID)
	if err != nil {
		return model.SecretKey{}, nil, errors.Tag(err)
	}
	return row, versions, nil
}

// listSecretKeyVersionsByRows 批量查询分页结果对应的版本列表。
func (l *SecretKeyLogic) listSecretKeyVersionsByRows(rows []model.SecretKey) (map[string][]model.SecretKeyVersion, error) {
	result := make(map[string][]model.SecretKeyVersion, len(rows))
	if len(rows) == 0 {
		return result, nil
	}
	uuids := make([]string, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.UUID) != "" {
			uuids = append(uuids, row.UUID)
		}
	}
	if len(uuids) == 0 {
		return result, nil
	}
	var versions []model.SecretKeyVersion
	readDB := l.Svc.ReadDB(svc.DatabaseMain)
	if err := readDB.
		Where("uuid IN ?", uuids).
		Order("updated_at DESC, id DESC").
		Find(&versions).Error; err != nil {
		return nil, errors.Tag(err)
	}
	for _, version := range versions {
		result[version.UUID] = append(result[version.UUID], version)
	}
	return result, nil
}

// listSecretKeyVersionsByUUID 查询单个 AppID 的全部版本。
func (l *SecretKeyLogic) listSecretKeyVersionsByUUID(uuid string) ([]model.SecretKeyVersion, error) {
	readDB := l.Svc.ReadDB(svc.DatabaseMain)
	var versions []model.SecretKeyVersion
	if err := readDB.
		Where("uuid = ?", strings.TrimSpace(uuid)).
		Order("updated_at DESC, id DESC").
		Find(&versions).Error; err != nil {
		return nil, errors.Tag(err)
	}
	return versions, nil
}

// validateSecretKeyRequiredVersions 校验启用中的主配置所依赖的稳定/灰度版本材料是否真正可用。
func (l *SecretKeyLogic) validateSecretKeyRequiredVersions(row model.SecretKey, versions []model.SecretKeyVersion) error {
	required := make(map[string]model.SecretKeyVersion, 2)
	for _, version := range versions {
		if version.KeyVersion == strings.TrimSpace(row.StableVersion) {
			required[version.KeyVersion] = version
		}
		if strings.TrimSpace(row.GrayVersion) != "" && row.GrayPercent > 0 && version.KeyVersion == strings.TrimSpace(row.GrayVersion) {
			required[version.KeyVersion] = version
		}
	}
	if _, ok := required[strings.TrimSpace(row.StableVersion)]; !ok {
		return errors.Errorf("稳定版本不存在")
	}
	for _, version := range required {
		if err := validateSecretKeyEnabledValues(buildSaveSecretKeyReq(row, version)); err != nil {
			return errors.Wrapf(err, "版本[%s]不可启用", version.KeyVersion)
		}
	}
	return nil
}

// validateSecretKeySaveReq 校验新增或编辑时的主配置和版本配置。
func validateSecretKeySaveReq(req *types.SaveSecretKeyReq, versions []model.SecretKeyVersion) error {
	if req == nil {
		return errors.Errorf("秘钥请求不能为空")
	}
	if err := req.Validate(); err != nil {
		return errors.Tag(err)
	}
	if err := validateSecretKeyRouteWithVersions(req, versions); err != nil {
		return errors.Tag(err)
	}
	return validateSecretKeyEnabledValues(req)
}

// validateSecretKeyRouteWithVersions 校验稳定版本和灰度版本的引用关系。
func validateSecretKeyRouteWithVersions(req *types.SaveSecretKeyReq, versions []model.SecretKeyVersion) error {
	if req == nil {
		return errors.Errorf("秘钥请求不能为空")
	}
	stableVersion := strings.TrimSpace(req.StableVersion)
	if stableVersion == "" {
		stableVersion = strings.TrimSpace(req.KeyVersion)
	}
	if stableVersion == "" {
		return errors.Errorf("稳定版本不能为空")
	}
	available := make(map[string]int)
	for _, version := range versions {
		available[strings.TrimSpace(version.KeyVersion)] = version.Status
	}
	if strings.TrimSpace(req.KeyVersion) != "" {
		available[strings.TrimSpace(req.KeyVersion)] = req.VersionStatus
	}
	stableStatus, ok := available[stableVersion]
	if !ok {
		return errors.Errorf("稳定版本[%s]不存在", stableVersion)
	}
	if req.Status == 1 && stableStatus != 1 {
		return errors.Errorf("启用中的应用必须绑定启用状态的稳定版本")
	}
	grayVersion := normalizedGrayVersion(req)
	grayPercent := normalizedGrayPercent(req)
	if grayPercent > 0 && grayVersion == "" {
		return errors.Errorf("灰度流量大于0时必须指定灰度版本")
	}
	if grayVersion == "" {
		return nil
	}
	grayStatus, ok := available[grayVersion]
	if !ok {
		return errors.Errorf("灰度版本[%s]不存在", grayVersion)
	}
	if req.Status == 1 && grayPercent > 0 && grayStatus != 1 {
		return errors.Errorf("启用中的应用不能把停用版本设置为灰度版本")
	}
	return nil
}

// buildSaveSecretKeyReq 使用主表和版本表拼出单版本校验请求。
func buildSaveSecretKeyReq(row model.SecretKey, version model.SecretKeyVersion) *types.SaveSecretKeyReq {
	return &types.SaveSecretKeyReq{
		ID:                     row.ID,
		UUID:                   row.UUID,
		Title:                  row.Title,
		KeyVersion:             version.KeyVersion,
		AESKeyRef:              version.AESKeyRef,
		AESIVRef:               version.AESIVRef,
		RSAPublicKeyUserRef:    version.RSAPublicKeyUserRef,
		RSAPublicKeyServerRef:  version.RSAPublicKeyServerRef,
		RSAPrivateKeyServerRef: version.RSAPrivateKeyServerRef,
		Status:                 row.Status,
		SignStatus:             row.SignStatus,
		CryptoStatus:           row.CryptoStatus,
		VersionStatus:          version.Status,
		StableVersion:          row.StableVersion,
		GrayVersion:            row.GrayVersion,
		GrayPercent:            row.GrayPercent,
		Remark:                 row.Remark,
	}
}

// secretKeyModelToItem 把主配置和版本列表转换成接口响应项。
func secretKeyModelToItem(row model.SecretKey, versions []model.SecretKeyVersion, selected *model.SecretKeyVersion, maskSecrets bool) types.SecretKeyItem {
	item := types.SecretKeyItem{
		ID:            row.ID,
		UUID:          row.UUID,
		Title:         row.Title,
		StableVersion: row.StableVersion,
		GrayVersion:   row.GrayVersion,
		GrayPercent:   row.GrayPercent,
		Status:        row.Status,
		SignStatus:    row.SignStatus,
		CryptoStatus:  row.CryptoStatus,
		VersionCount:  len(versions),
		Remark:        row.Remark,
		CreatedAt:     corelogic.FormatDateTime(row.CreatedAt),
		UpdatedAt:     corelogic.FormatDateTime(row.UpdatedAt),
	}
	if selected != nil {
		item.KeyVersion = selected.KeyVersion
		item.VersionStatus = selected.Status
		item.AESKeyRef = maybeMaskSecretKeyValue(selected.AESKeyRef, maskSecrets)
		item.AESIVRef = maybeMaskSecretKeyValue(selected.AESIVRef, maskSecrets)
		item.RSAPublicKeyUserRef = maybeMaskSecretKeyValue(selected.RSAPublicKeyUserRef, maskSecrets)
		item.RSAPublicKeyServerRef = maybeMaskSecretKeyValue(selected.RSAPublicKeyServerRef, maskSecrets)
		item.RSAPrivateKeyServerRef = maybeMaskSecretKeyValue(selected.RSAPrivateKeyServerRef, maskSecrets)
		item.SecretMasked = maskSecrets
	}
	if len(versions) > 0 {
		item.VersionList = make([]types.SecretKeyVersionItem, 0, len(versions))
		for _, version := range versions {
			item.VersionList = append(item.VersionList, secretKeyVersionToItem(version, row, maskSecrets))
		}
	}
	return item
}

// secretKeyVersionToItem 把秘钥版本模型转换成接口响应项。
func secretKeyVersionToItem(version model.SecretKeyVersion, row model.SecretKey, maskSecrets bool) types.SecretKeyVersionItem {
	return types.SecretKeyVersionItem{
		ID:                     version.ID,
		KeyVersion:             version.KeyVersion,
		AESKeyRef:              maybeMaskSecretKeyValue(version.AESKeyRef, maskSecrets),
		AESIVRef:               maybeMaskSecretKeyValue(version.AESIVRef, maskSecrets),
		RSAPublicKeyUserRef:    maybeMaskSecretKeyValue(version.RSAPublicKeyUserRef, maskSecrets),
		RSAPublicKeyServerRef:  maybeMaskSecretKeyValue(version.RSAPublicKeyServerRef, maskSecrets),
		RSAPrivateKeyServerRef: maybeMaskSecretKeyValue(version.RSAPrivateKeyServerRef, maskSecrets),
		SecretMasked:           maskSecrets,
		Status:                 version.Status,
		IsStable:               strings.TrimSpace(version.KeyVersion) == strings.TrimSpace(row.StableVersion),
		IsGray:                 strings.TrimSpace(version.KeyVersion) == strings.TrimSpace(row.GrayVersion) && row.GrayPercent > 0,
		Remark:                 version.Remark,
		CreatedAt:              corelogic.FormatDateTime(version.CreatedAt),
		UpdatedAt:              corelogic.FormatDateTime(version.UpdatedAt),
	}
}

// selectSecretKeyVersion 根据指定版本号或稳定版本选择当前默认展示版本。
func selectSecretKeyVersion(versions []model.SecretKeyVersion, versionHint string, stableVersion string) *model.SecretKeyVersion {
	versionHint = strings.TrimSpace(versionHint)
	stableVersion = strings.TrimSpace(stableVersion)
	for _, version := range versions {
		if versionHint != "" && strings.TrimSpace(version.KeyVersion) == versionHint {
			return new(version)
		}
	}
	for _, version := range versions {
		if stableVersion != "" && strings.TrimSpace(version.KeyVersion) == stableVersion {
			return new(version)
		}
	}
	if len(versions) == 0 {
		return nil
	}
	return new(versions[0])
}

// maybeMaskSecretKeyValue 根据场景决定是否对绝对路径脱敏。
func maybeMaskSecretKeyValue(value string, maskSecrets bool) string {
	if !maskSecrets {
		return value
	}
	return maskSecretKeyValue(value)
}

// normalizedGrayVersion 归一化灰度版本配置；和稳定版本相同或灰度流量为 0 时视为未配置。
func normalizedGrayVersion(req *types.SaveSecretKeyReq) string {
	if req == nil {
		return ""
	}
	grayVersion := strings.TrimSpace(req.GrayVersion)
	stableVersion := strings.TrimSpace(req.StableVersion)
	if stableVersion == "" {
		stableVersion = strings.TrimSpace(req.KeyVersion)
	}
	if req.GrayPercent <= 0 || grayVersion == "" || grayVersion == stableVersion {
		return ""
	}
	return grayVersion
}

// normalizedGrayPercent 归一化灰度比例；未配置灰度版本时强制归零。
func normalizedGrayPercent(req *types.SaveSecretKeyReq) int {
	if req == nil {
		return 0
	}
	if normalizedGrayVersion(req) == "" {
		return 0
	}
	if req.GrayPercent < 0 {
		return 0
	}
	if req.GrayPercent > 100 {
		return 100
	}
	return req.GrayPercent
}

// shouldRefreshGraySalt 当灰度版本或比例发生变化时刷新盐值，确保路由落点稳定且可控。
func shouldRefreshGraySalt(oldRow model.SecretKey, req *types.SaveSecretKeyReq) bool {
	return strings.TrimSpace(oldRow.GrayVersion) != normalizedGrayVersion(req) ||
		oldRow.GrayPercent != normalizedGrayPercent(req)
}

// buildSecretKeyGraySalt 为灰度版本生成稳定盐值；未启用灰度时返回空字符串。
func buildSecretKeyGraySalt(grayVersion string) string {
	if strings.TrimSpace(grayVersion) == "" {
		return ""
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("gray_%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// maskSecretKeyValue 对秘钥文件路径做后端脱敏，避免把完整绝对路径直接下发到前端列表。
func maskSecretKeyValue(value string) string {
	text := strings.TrimSpace(value)
	if text == "" {
		return ""
	}
	normalized := strings.ReplaceAll(text, "\\", "/")
	parts := strings.Split(normalized, "/")
	fileName := parts[len(parts)-1]
	if fileName == "" {
		fileName = normalized
	}
	if len(fileName) <= 8 {
		return fileName[:min(len(fileName), 2)] + "****"
	}
	return fileName[:4] + "****" + fileName[len(fileName)-4:]
}
