package security

import (
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"

	utils "github.com/Is999/go-utils"
	"github.com/Is999/go-utils/errors"
)

const (
	// SignatureTypeMD5 表示 MD5 签名方式，对应 laravel-admin 的 X-Signature=M。
	SignatureTypeMD5 = "M"
	// SignatureTypeAES 表示 AES 签名方式，对应 laravel-admin 的 X-Signature=A。
	SignatureTypeAES = "A"
	// SignatureTypeRSA 表示 RSA 签名方式，对应 laravel-admin 的 X-Signature=R。
	SignatureTypeRSA = "R"

	// CryptoTypeAES 表示 AES 加解密方式，对应 laravel-admin 的 X-Crypto=A。
	CryptoTypeAES = "A"
	// CryptoTypeRSA 表示 RSA 加解密方式，对应 laravel-admin 的 X-Crypto=R。
	CryptoTypeRSA = "R"
)

// Signer 定义签名与验签能力，供签名中间件按请求头动态选择实现。
type Signer interface {
	Sign(data string) (string, error)       // Sign 对待签名字符串生成签名值
	Verify(data, sign string) (bool, error) // Verify 校验待签名字符串与签名值是否匹配
}

// Cryptor 定义加密与解密能力，供加密中间件按请求头动态选择实现。
type Cryptor interface {
	Encrypt(data string) (string, error) // Encrypt 加密明文字符串
	Decrypt(data string) (string, error) // Decrypt 解密密文字符串
}

// MD5Signer 实现 laravel-admin 的简单 MD5 签名。
type MD5Signer struct{}

// Sign 对待签名字符串做 MD5 摘要。
func (MD5Signer) Sign(data string) (string, error) {
	return utils.Md5(data), nil
}

// Verify 使用常量时间比较校验 MD5 签名。
func (s MD5Signer) Verify(data, sign string) (bool, error) {
	expected, err := s.Sign(data)
	if err != nil {
		return false, errors.Tag(err)
	}
	return expected == sign, nil
}

// AESCipher 实现 AES-256-CBC 兼容的加解密与签名能力。
type AESCipher struct {
	cipher *utils.Cipher // cipher 是 go-utils AES-CBC 加解密器
}

// NewAESCipher 创建 AES-CBC 加解密器。
func NewAESCipher(key, iv string) (*AESCipher, error) {
	cipherObj, err := utils.AES(key, utils.WithIV(iv))
	if err != nil {
		return nil, errors.Wrap(err, "初始化AES加解密器失败")
	}
	return &AESCipher{cipher: cipherObj}, nil
}

// Sign 先计算 SHA256 摘要，再用 AES 加密摘要字符串，保持与 laravel-admin AesService 一致。
func (c *AESCipher) Sign(data string) (string, error) {
	return c.Encrypt(utils.Sha256(data))
}

// Verify 解密签名后与 SHA256 摘要字符串比较。
func (c *AESCipher) Verify(data, sign string) (bool, error) {
	decrypted, err := c.Decrypt(sign)
	if err != nil {
		return false, errors.Tag(err)
	}
	return decrypted == utils.Sha256(data), nil
}

// Encrypt 使用 AES-CBC 与 PKCS7 填充加密明文，输出 base64 密文。
func (c *AESCipher) Encrypt(data string) (string, error) {
	return c.cipher.Encrypt(data, utils.CBC, base64.StdEncoding.EncodeToString, utils.Pkcs7Padding)
}

// Decrypt 解密 base64 AES-CBC 密文并移除 PKCS7 填充。
func (c *AESCipher) Decrypt(data string) (string, error) {
	return c.cipher.Decrypt(data, utils.CBC, base64.StdEncoding.DecodeString, utils.Pkcs7UnPadding)
}

// RSASigner 实现 RSA SHA256 签名与验签能力。
type RSASigner struct {
	privateKey *utils.RSA // 服务端私钥，用于响应签名
	publicKey  *utils.RSA // 用户公钥，用于请求验签
}

// NewRSASigner 创建 RSA 签名器；签名场景传私钥 PEM，验签场景传公钥 PEM。
func NewRSASigner(privatePEM string, publicPEM string) (*RSASigner, error) {
	signer := &RSASigner{}
	if privatePEM != "" {
		privateKey, err := utils.NewPriRSA(privatePEM)
		if err != nil {
			return nil, errors.Wrap(err, "初始化RSA签名私钥失败")
		}
		signer.privateKey = privateKey
	}
	if publicPEM != "" {
		publicKey, err := utils.NewPubRSA(publicPEM)
		if err != nil {
			return nil, errors.Wrap(err, "初始化RSA验签公钥失败")
		}
		signer.publicKey = publicKey
	}
	return signer, nil
}

// Sign 使用服务端私钥对待签名字符串做 RSA-SHA256 签名。
func (s *RSASigner) Sign(data string) (string, error) {
	if s.privateKey == nil {
		return "", errors.New("RSA私钥未配置")
	}
	return s.privateKey.Sign(data, crypto.SHA256, base64.StdEncoding.EncodeToString)
}

// Verify 使用用户公钥校验 RSA-SHA256 签名。
func (s *RSASigner) Verify(data, sign string) (bool, error) {
	if s.publicKey == nil {
		return false, errors.New("RSA公钥未配置")
	}
	if err := s.publicKey.Verify(data, sign, crypto.SHA256, base64.StdEncoding.DecodeString); err != nil {
		if errors.Is(err, rsa.ErrVerification) {
			return false, nil
		}
		return false, errors.Tag(err)
	}
	return true, nil
}

// RSACipher 实现 RSA PKCS#1 v1.5 分段加解密能力。
type RSACipher struct {
	privateKey *utils.RSA // 服务端私钥，用于解密请求数据
	publicKey  *utils.RSA // 用户公钥，用于加密响应数据
}

// NewRSACipher 创建 RSA 加解密器；加密场景传公钥 PEM，解密场景传私钥 PEM。
func NewRSACipher(privatePEM string, publicPEM string) (*RSACipher, error) {
	cipherObj := &RSACipher{}
	if privatePEM != "" {
		privateKey, err := utils.NewPriRSA(privatePEM)
		if err != nil {
			return nil, errors.Wrap(err, "初始化RSA解密私钥失败")
		}
		cipherObj.privateKey = privateKey
	}
	if publicPEM != "" {
		publicKey, err := utils.NewPubRSA(publicPEM)
		if err != nil {
			return nil, errors.Wrap(err, "初始化RSA加密公钥失败")
		}
		cipherObj.publicKey = publicKey
	}
	return cipherObj, nil
}

// Encrypt 使用用户公钥分段加密响应明文。
func (c *RSACipher) Encrypt(data string) (string, error) {
	if c.publicKey == nil {
		return "", errors.New("RSA公钥未配置")
	}
	return c.publicKey.Encrypt(data, base64.StdEncoding.EncodeToString)
}

// Decrypt 使用服务端私钥分段解密请求密文。
func (c *RSACipher) Decrypt(data string) (string, error) {
	if c.privateKey == nil {
		return "", errors.New("RSA私钥未配置")
	}
	return c.privateKey.Decrypt(data, base64.StdEncoding.DecodeString)
}

// ParseRSAPrivateKey 解析 PKCS#1 或 PKCS#8 私钥 PEM。
func ParseRSAPrivateKey(pemText string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, errors.Errorf("RSA私钥PEM格式不合法")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.Tag(err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.Errorf("PEM内容不是RSA私钥")
	}
	return key, nil
}

// ParseRSAPublicKey 解析 PKIX 或 PKCS#1 公钥 PEM。
func ParseRSAPublicKey(pemText string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, errors.Errorf("RSA公钥PEM格式不合法")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err == nil {
		key, ok := parsed.(*rsa.PublicKey)
		if !ok {
			return nil, errors.Errorf("PEM内容不是RSA公钥")
		}
		return key, nil
	}
	key, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		return nil, errors.Tag(err)
	}
	return key, nil
}
