package secretkey

import (
	"crypto/rsa"
	"strings"
	"time"

	corelogic "admin/internal/logic"
	"admin/internal/model"
	"admin/internal/security"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
)

// secretKeyPayloadChecker 保存一次秘钥校验的请求和聚合结果。
type secretKeyPayloadChecker struct {
	req    *types.SaveSecretKeyReq    // req 表示已保证非空的校验请求。
	result types.SecretKeyCheckResult // result 表示持续更新的聚合结果。
	items  []types.SecretKeyCheckItem // items 表示按执行顺序产生的分项结果。
}

// secretKeyCryptoMaterial 保存静态材料校验后供运行态自检复用的密钥内容。
type secretKeyCryptoMaterial struct {
	signEnabled      bool            // signEnabled 表示是否需要校验签名链路。
	cryptoEnabled    bool            // cryptoEnabled 表示是否需要校验加解密链路。
	aesKeyText       string          // aesKeyText 表示从安全文件读取的 AES KEY。
	aesIVText        string          // aesIVText 表示从安全文件读取的 AES IV。
	userPublicPEM    string          // userPublicPEM 表示用户 RSA 公钥 PEM。
	serverPublicPEM  string          // serverPublicPEM 表示服务端 RSA 公钥 PEM。
	serverPrivatePEM string          // serverPrivatePEM 表示服务端 RSA 私钥 PEM。
	serverPublicKey  *rsa.PublicKey  // serverPublicKey 表示已解析或派生的服务端公钥。
	serverPrivateKey *rsa.PrivateKey // serverPrivateKey 表示已解析的服务端私钥。
}

// newSecretKeyPayloadChecker 初始化一次秘钥校验的稳定输出字段。
func newSecretKeyPayloadChecker(req *types.SaveSecretKeyReq, runtimeCheck bool) *secretKeyPayloadChecker {
	if req == nil {
		req = &types.SaveSecretKeyReq{}
	}
	mode := "validate"
	if runtimeCheck {
		mode = "self_check"
	}
	return &secretKeyPayloadChecker{
		req: req,
		result: types.SecretKeyCheckResult{
			UUID:           strings.TrimSpace(req.UUID),
			Title:          strings.TrimSpace(req.Title),
			KeyVersion:     strings.TrimSpace(req.KeyVersion),
			Mode:           mode,
			Status:         req.Status,
			CanSave:        true,
			CanEnable:      true,
			RuntimeChecked: runtimeCheck,
		},
		items: make([]types.SecretKeyCheckItem, 0, 24),
	}
}

// appendItem 追加普通校验项；失败项只阻止启用，不阻止保存草稿。
func (c *secretKeyPayloadChecker) appendItem(key, label string, passed bool, successMessage, failMessage string) {
	message := successMessage
	level := "success"
	if !passed {
		message = failMessage
		level = "error"
	}
	c.items = append(c.items, types.SecretKeyCheckItem{
		Key:     key,
		Label:   label,
		Passed:  passed,
		Level:   level,
		Message: message,
	})
	if !passed {
		c.result.CanEnable = false
	}
}

// appendError 追加会同时阻止保存和启用的结构性错误。
func (c *secretKeyPayloadChecker) appendError(key, label, safeMessage string) {
	errText := strings.TrimSpace(safeMessage)
	if errText == "" {
		errText = "校验失败，请检查配置"
	}
	c.items = append(c.items, types.SecretKeyCheckItem{
		Key:     key,
		Label:   label,
		Passed:  false,
		Level:   "error",
		Message: errText,
	})
	c.result.CanSave = false
	c.result.CanEnable = false
}

// validateSecretKeyStaticFields 校验标识、标题、版本和灰度路由关系。
func (c *secretKeyPayloadChecker) validateSecretKeyStaticFields(versions []model.SecretKeyVersion) {
	if c.result.UUID == "" {
		c.appendError("uuid", "秘钥标识", "秘钥标识不能为空")
	}
	if strings.TrimSpace(c.req.Title) == "" {
		c.appendError("title", "秘钥标题", "秘钥标题不能为空")
	}
	if c.result.KeyVersion == "" {
		c.appendError("key_version", "秘钥版本", "秘钥版本不能为空")
	}
	if err := validateSecretKeyRouteWithVersions(c.req, versions); err != nil {
		c.appendError("route.version", "版本路由配置", err.Error())
	} else {
		c.appendItem("route.version", "版本路由配置", true, "稳定版本与灰度版本配置合法", "")
	}
}

// validateSecretKeyCryptoMaterial 校验 AES/RSA 文件、格式和服务端密钥配对关系。
func (c *secretKeyPayloadChecker) validateSecretKeyCryptoMaterial() secretKeyCryptoMaterial {
	material := secretKeyCryptoMaterial{
		signEnabled:   secretKeySignEnabled(c.req),
		cryptoEnabled: secretKeyCryptoEnabled(c.req),
	}
	c.validateSecretKeyAESMaterial(&material)
	c.validateSecretKeyRSACommonMaterial(&material)
	c.validateSecretKeyRSAServerPair(&material)
	return material
}

// validateSecretKeyAESMaterial 校验 AES 文件路径、可读性和长度。
func (c *secretKeyPayloadChecker) validateSecretKeyAESMaterial(material *secretKeyCryptoMaterial) {
	if !material.cryptoEnabled {
		c.appendItem("crypto_status", "加密解密状态", true, "当前已关闭加密解密链路，跳过 AES 校验", "")
		return
	}
	if _, err := normalizeSecretRef(c.req.AESKeyRef); err != nil {
		c.appendError("aes_key_ref.path", "AES KEY路径", "开启加密解密后，AES KEY 必须填写绝对路径，且不能直接录入明文或 PEM")
	} else {
		c.appendItem("aes_key_ref.path", "AES KEY路径", true, "AES KEY 路径格式正确", "")
	}
	if _, err := normalizeSecretRef(c.req.AESIVRef); err != nil {
		c.appendError("aes_iv_ref.path", "AES IV路径", "开启加密解密后，AES IV 必须填写绝对路径，且不能直接录入明文")
	} else {
		c.appendItem("aes_iv_ref.path", "AES IV路径", true, "AES IV 路径格式正确", "")
	}
	var err error
	material.aesKeyText, err = normalizeSecretText(c.req.AESKeyRef)
	if err != nil {
		c.appendError("aes_key_ref.file", "AES KEY文件", "AES KEY 文件不存在、不可读或内容为空")
	} else {
		c.appendItem("aes_key_ref.file", "AES KEY文件", true, "AES KEY 文件可读取", "")
	}
	material.aesIVText, err = normalizeSecretText(c.req.AESIVRef)
	if err != nil {
		c.appendError("aes_iv_ref.file", "AES IV文件", "AES IV 文件不存在、不可读或内容为空")
	} else {
		c.appendItem("aes_iv_ref.file", "AES IV文件", true, "AES IV 文件可读取", "")
	}
	aesKeyLengthPassed := len(material.aesKeyText) == 16 || len(material.aesKeyText) == 24 || len(material.aesKeyText) == 32
	c.appendItem("aes_key_ref.length", "AES KEY长度", aesKeyLengthPassed, "AES KEY 长度合法", "AES KEY长度必须是16、24或32位")
	c.appendItem("aes_iv_ref.length", "AES IV长度", len(material.aesIVText) == 16, "AES IV 长度合法", "AES IV长度必须是16位")
}

// validateSecretKeyRSACommonMaterial 校验签名和加解密共用的用户公钥与服务端私钥。
func (c *secretKeyPayloadChecker) validateSecretKeyRSACommonMaterial(material *secretKeyCryptoMaterial) {
	if !material.signEnabled && !material.cryptoEnabled {
		c.appendItem("sign_status", "签名验签状态", true, "当前已关闭签名验签链路，跳过 RSA 材料校验", "")
		return
	}
	if _, err := normalizeSecretRef(c.req.RSAPublicKeyUserRef); err != nil {
		c.appendError("rsa_public_key_user_ref.path", "用户 RSA公钥路径", "启用签名验签或加密解密后，用户 RSA 公钥必须填写绝对路径，且不能直接录入 PEM")
	} else {
		c.appendItem("rsa_public_key_user_ref.path", "用户 RSA公钥路径", true, "用户 RSA 公钥路径格式正确", "")
	}
	if _, err := normalizeSecretRef(c.req.RSAPrivateKeyServerRef); err != nil {
		c.appendError("rsa_private_key_server_ref.path", "服务端 RSA私钥路径", "启用签名验签或加密解密后，服务端 RSA 私钥必须填写绝对路径，且不能直接录入 PEM")
	} else {
		c.appendItem("rsa_private_key_server_ref.path", "服务端 RSA 私钥路径", true, "服务端 RSA 私钥路径格式正确", "")
	}
	var err error
	material.userPublicPEM, err = resolvePEMText(c.req.RSAPublicKeyUserRef)
	if err != nil {
		c.appendError("rsa_public_key_user_ref.file", "用户 RSA公钥文件", "用户 RSA 公钥文件不存在、不可读或内容不是有效 PEM")
	} else {
		c.appendItem("rsa_public_key_user_ref.file", "用户 RSA公钥文件", true, "用户 RSA 公钥文件可读取", "")
	}
	material.serverPrivatePEM, err = resolvePEMText(c.req.RSAPrivateKeyServerRef)
	if err != nil {
		c.appendError("rsa_private_key_server_ref.file", "服务端 RSA私钥文件", "服务端 RSA 私钥文件不存在、不可读或内容不是有效 PEM")
	} else {
		c.appendItem("rsa_private_key_server_ref.file", "服务端 RSA私钥文件", true, "服务端 RSA 私钥文件可读取", "")
	}
	if _, userPublicErr := security.ParseRSAPublicKey(material.userPublicPEM); userPublicErr != nil {
		c.appendError("rsa_public_key_user_ref.pem", "用户 RSA公钥格式", "用户 RSA 公钥 PEM 格式不合法")
	} else {
		c.appendItem("rsa_public_key_user_ref.pem", "用户 RSA公钥格式", true, "用户 RSA 公钥 PEM 格式正确", "")
	}
	material.serverPrivateKey, err = security.ParseRSAPrivateKey(material.serverPrivatePEM)
	if err != nil {
		c.appendError("rsa_private_key_server_ref.pem", "服务端 RSA私钥格式", "服务端 RSA 私钥 PEM 格式不合法")
	} else {
		c.appendItem("rsa_private_key_server_ref.pem", "服务端 RSA私钥格式", true, "服务端 RSA 私钥 PEM 格式正确", "")
	}
}

// validateSecretKeyRSAServerPair 校验签名使用的服务端公钥来源和公私钥配对关系。
func (c *secretKeyPayloadChecker) validateSecretKeyRSAServerPair(material *secretKeyCryptoMaterial) {
	if !material.signEnabled {
		return
	}
	if strings.TrimSpace(c.req.RSAPublicKeyServerRef) == "" {
		if material.serverPrivateKey == nil {
			c.appendError("rsa_public_key_server_ref.derived", "服务端 RSA公钥", "服务端 RSA 私钥格式未通过，无法派生公钥")
		} else {
			var err error
			material.serverPublicPEM, err = deriveRSAPublicPEMFromPrivateKey(material.serverPrivateKey)
			if err != nil {
				c.appendError("rsa_public_key_server_ref.derived", "服务端 RSA公钥", "服务端 RSA 公钥派生失败")
			} else {
				material.serverPublicKey = &material.serverPrivateKey.PublicKey
				c.appendItem("rsa_public_key_server_ref.derived", "服务端 RSA公钥", true, "未配置公钥路径，已由服务端私钥派生", "")
			}
		}
	} else {
		if _, err := normalizeSecretRef(c.req.RSAPublicKeyServerRef); err != nil {
			c.appendError("rsa_public_key_server_ref.path", "服务端 RSA公钥路径", "服务端 RSA 公钥路径格式错误，不能直接录入 PEM")
		} else {
			c.appendItem("rsa_public_key_server_ref.path", "服务端 RSA 公钥路径", true, "服务端 RSA 公钥路径格式正确", "")
		}
		var err error
		material.serverPublicPEM, err = resolvePEMText(c.req.RSAPublicKeyServerRef)
		if err != nil {
			c.appendError("rsa_public_key_server_ref.file", "服务端 RSA公钥文件", "服务端 RSA 公钥文件不存在、不可读或内容不是有效 PEM")
		} else {
			c.appendItem("rsa_public_key_server_ref.file", "服务端 RSA公钥文件", true, "服务端 RSA 公钥文件可读取", "")
		}
		material.serverPublicKey, err = security.ParseRSAPublicKey(material.serverPublicPEM)
		if err != nil {
			c.appendError("rsa_public_key_server_ref.pem", "服务端 RSA公钥格式", "服务端 RSA 公钥 PEM 格式不合法")
		} else {
			c.appendItem("rsa_public_key_server_ref.pem", "服务端 RSA公钥格式", true, "服务端 RSA 公钥 PEM 格式正确", "")
		}
	}
	if material.serverPublicKey != nil && material.serverPrivateKey != nil {
		rsaPairPassed := material.serverPublicKey.N.Cmp(material.serverPrivateKey.N) == 0 && material.serverPublicKey.E == material.serverPrivateKey.E
		c.appendItem("rsa_server_pair.match", "服务端 RSA配对", rsaPairPassed, "服务端 RSA 公私钥配对正确", "服务端 RSA 公钥与私钥不是同一对")
	} else {
		c.appendError("rsa_server_pair.match", "服务端 RSA配对", "服务端 RSA 公私钥格式未通过，暂时无法判断是否配对")
	}
}

// validateSecretKeyRuntimeSelfCheck 执行 AES、RSA 签名验签和请求解密链路自检。
func (l *SecretKeyLogic) validateSecretKeyRuntimeSelfCheck(checker *secretKeyPayloadChecker, material secretKeyCryptoMaterial) {
	if material.cryptoEnabled {
		if aesCipher, err := security.NewAESCipher(material.aesKeyText, material.aesIVText); err != nil {
			checker.appendError("runtime.aes.init", "AES运行态初始化", "AES 运行态初始化失败，请检查 AES KEY 与 IV 内容")
		} else {
			checker.appendItem("runtime.aes.init", "AES运行态初始化", true, "AES 运行态初始化成功", "")
			const aesPlaintext = "admin-secret-check"
			cipherText, encryptErr := aesCipher.Encrypt(aesPlaintext)
			if encryptErr != nil {
				checker.appendError("runtime.aes.encrypt", "AES加密自检", "AES 加密自检失败")
			} else if plainText, decryptErr := aesCipher.Decrypt(cipherText); decryptErr != nil {
				checker.appendError("runtime.aes.decrypt", "AES解密自检", "AES 解密自检失败")
			} else {
				checker.appendItem("runtime.aes.decrypt", "AES加解密自检", plainText == aesPlaintext, "AES 加解密链路可用", "AES 解密结果与原文不一致")
			}
		}
	}

	if material.signEnabled {
		l.validateSecretKeyRSASignRuntime(checker, material)
	}
	if material.cryptoEnabled {
		l.validateSecretKeyRSACryptoRuntime(checker, material)
	}
}

// validateSecretKeyRSASignRuntime 执行服务端 RSA 签名与验签自检。
func (l *SecretKeyLogic) validateSecretKeyRSASignRuntime(checker *secretKeyPayloadChecker, material secretKeyCryptoMaterial) {
	signer, signerErr := security.NewRSASigner(material.serverPrivatePEM, "")
	if signerErr != nil {
		l.logSecretKeySignCheckFailure(checker.result.UUID, checker.result.KeyVersion, "runtime.rsa.signer", RSAServerPrivateKey, signerErr)
		checker.appendError("runtime.rsa.signer", "RSA签名器初始化", "RSA 签名器初始化失败，请检查服务端私钥")
		return
	}
	checker.appendItem("runtime.rsa.signer", "RSA签名器初始化", true, "RSA 签名器初始化成功", "")
	signValue, signErr := signer.Sign(secretKeyRSASignCheckPayload)
	if signErr != nil {
		l.logSecretKeySignCheckFailure(checker.result.UUID, checker.result.KeyVersion, "runtime.rsa.sign", RSAServerPrivateKey, signErr)
		checker.appendError("runtime.rsa.sign", "RSA签名自检", "RSA 签名自检失败")
		return
	}
	checker.appendItem("runtime.rsa.sign", "RSA签名自检", true, "RSA 签名链路可用", "")
	verifySigner, verifyErr := security.NewRSASigner("", material.serverPublicPEM)
	if verifyErr != nil {
		l.logSecretKeySignCheckFailure(checker.result.UUID, checker.result.KeyVersion, "runtime.rsa.verify_init", RSAServerPublicKey, verifyErr)
		checker.appendError("runtime.rsa.verify_init", "RSA验签器初始化", "RSA 验签器初始化失败，请检查服务端公钥")
		return
	}
	verified, verifyRunErr := verifySigner.Verify(secretKeyRSASignCheckPayload, signValue)
	if verifyRunErr != nil {
		l.logSecretKeySignCheckFailure(checker.result.UUID, checker.result.KeyVersion, "runtime.rsa.verify", RSAServerPublicKey, verifyRunErr)
		checker.appendError("runtime.rsa.verify", "RSA验签自检", "RSA 验签自检失败")
		return
	}
	if !verified {
		l.logSecretKeySignCheckFailure(checker.result.UUID, checker.result.KeyVersion, "runtime.rsa.verify", RSAServerPublicKey, errors.New("RSA验签结果不匹配"))
	}
	checker.appendItem("runtime.rsa.verify", "RSA验签自检", verified, "RSA 验签链路可用", "RSA 验签失败，请确认服务端公钥与服务端私钥是否对应")
}

// validateSecretKeyRSACryptoRuntime 执行响应加密器初始化和请求解密自检。
func (l *SecretKeyLogic) validateSecretKeyRSACryptoRuntime(checker *secretKeyPayloadChecker, material secretKeyCryptoMaterial) {
	rsaCipher, rsaCipherErr := security.NewRSACipher(material.serverPrivatePEM, material.userPublicPEM)
	if rsaCipherErr != nil {
		checker.appendError("runtime.rsa.cipher_init", "RSA加解密器初始化", "RSA 加解密器初始化失败，请检查服务端私钥与用户公钥")
		return
	}
	checker.appendItem("runtime.rsa.cipher_init", "RSA加解密器初始化", true, "RSA 加解密器初始化成功", "")
	const rsaPlaintext = "admin-rsa-check"
	if _, encryptErr := rsaCipher.Encrypt(rsaPlaintext); encryptErr != nil {
		checker.appendError("runtime.rsa.encrypt", "RSA加密自检", "RSA 加密自检失败")
		return
	}
	requestDecryptPassed, decryptErr := runSecretKeyRSARequestDecryptSelfCheck(material.serverPrivatePEM)
	if decryptErr != nil {
		checker.appendError("runtime.rsa.decrypt", "RSA解密自检", "RSA 解密自检失败")
		return
	}
	checker.appendItem("runtime.rsa.decrypt", "RSA请求解密自检", requestDecryptPassed, "RSA 请求解密链路可用", "RSA 请求解密失败，请确认服务端公钥与服务端私钥是否对应")
}

// finish 汇总分项结果并补齐时间信息。
func (c *secretKeyPayloadChecker) finish(start time.Time) types.SecretKeyCheckResult {
	c.result.Items = c.items
	c.result.AllPassed = true
	for _, item := range c.items {
		if !item.Passed {
			c.result.AllPassed = false
			break
		}
	}
	c.result.CanSave = c.result.CanSave && len(c.items) > 0
	if c.req.Status != 1 {
		c.result.CanEnable = false
	} else {
		c.result.CanEnable = c.result.CanEnable && c.result.AllPassed
	}
	c.result.CheckedAt = corelogic.FormatDateTime(time.Now())
	c.result.DurationMs = time.Since(start).Milliseconds()
	return c.result
}

// checkSecretKeyPayload 统一执行秘钥静态校验与运行态自检，供预检、自检、启用前校验复用。
func (l *SecretKeyLogic) checkSecretKeyPayload(req *types.SaveSecretKeyReq, versions []model.SecretKeyVersion, refreshCache bool, runtimeCheck bool) types.SecretKeyCheckResult {
	start := time.Now()
	checker := newSecretKeyPayloadChecker(req, runtimeCheck)
	checker.validateSecretKeyStaticFields(versions)
	material := checker.validateSecretKeyCryptoMaterial()
	if refreshCache && checker.result.UUID != "" {
		if err := l.RenewSecretKeyCache(checker.result.UUID); err != nil {
			checker.appendError("cache.refresh", "缓存刷新", "刷新秘钥缓存失败，请检查 Redis、数据库和当前秘钥配置")
		} else {
			checker.result.CacheRefreshed = true
			checker.appendItem("cache.refresh", "缓存刷新", true, "秘钥缓存刷新成功", "")
		}
	}
	if runtimeCheck && checker.result.UUID != "" {
		l.validateSecretKeyRuntimeSelfCheck(checker, material)
	}
	return checker.finish(start)
}
