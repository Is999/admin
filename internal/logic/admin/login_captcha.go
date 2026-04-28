package admin

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"html"
	"math/big"
	"strings"
	"time"

	codes "admin/common/codes"
	i18n "admin/common/i18n"
	keys "admin/common/rediskeys"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"github.com/google/uuid"
)

const (
	// loginCaptchaTTLSeconds 表示登录图形验证码有效期（秒）。
	loginCaptchaTTLSeconds = 300
	// loginCaptchaLength 表示图形验证码长度。
	loginCaptchaLength = 4
)

var loginCaptchaAlphabet = []rune("ABCDEFGHJKLMNPQRSTUVWXYZ23456789")

// BuildLoginCaptcha 生成登录图形验证码并写入 Redis。
func (l *AdminLogic) BuildLoginCaptcha() *types.BizResult {
	if l.Redis() == nil {
		return types.NewBizResult(codes.ServerError).
			SetI18nMessage(i18n.MsgKeyInternalError).
			WithError(errors.New("AdminLogic.BuildLoginCaptcha Redis 未初始化"))
	}
	key := strings.ReplaceAll(uuid.NewString(), "-", "")
	code, err := generateLoginCaptchaCode(loginCaptchaLength)
	if err != nil {
		return types.NewBizResult(codes.ServerError).
			SetI18nMessage(i18n.MsgKeyInternalError).
			WithError(errors.Wrap(err, "AdminLogic.BuildLoginCaptcha 生成验证码失败"))
	}
	cacheKey := l.AppRedisKey(fmt.Sprintf(keys.LoginCaptcha, key))
	if err = l.Redis().Set(l.Ctx, cacheKey, strings.ToLower(code), loginCaptchaTTLSeconds*time.Second).Err(); err != nil {
		return types.NewBizResult(codes.ServerError).
			SetI18nMessage(i18n.MsgKeyInternalErrorFormat).
			WithError(errors.Wrap(err, "AdminLogic.BuildLoginCaptcha 写入验证码缓存失败"))
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeySuccess).
		WithData(&types.LoginCaptchaResp{
			Key:           key,
			Image:         buildLoginCaptchaSVGDataURL(code),
			ExpireSeconds: loginCaptchaTTLSeconds,
		})
}

// VerifyLoginCaptcha 校验登录图形验证码；校验通过或失败后都会消费当前验证码，避免重复重放。
func (l *AdminLogic) VerifyLoginCaptcha(key string, captcha string) *types.BizResult {
	if l.Redis() == nil {
		return types.NewBizResult(codes.ServerError).
			SetI18nMessage(i18n.MsgKeyInternalError).
			WithError(errors.New("AdminLogic.VerifyLoginCaptcha Redis 未初始化"))
	}
	key = strings.TrimSpace(key)
	captcha = strings.ToLower(strings.TrimSpace(captcha))
	if key == "" || captcha == "" {
		return invalidLoginCaptchaResult(errors.New("登录验证码不能为空"))
	}
	cacheKey := l.AppRedisKey(fmt.Sprintf(keys.LoginCaptcha, key))
	saved, err := l.Redis().GetDel(l.Ctx, cacheKey).Result()
	if err != nil {
		return invalidLoginCaptchaResult(errors.Wrap(err, "AdminLogic.VerifyLoginCaptcha 读取验证码缓存失败"))
	}
	if strings.TrimSpace(saved) == "" || !strings.EqualFold(saved, captcha) {
		return invalidLoginCaptchaResult(errors.Errorf("AdminLogic.VerifyLoginCaptcha 验证码错误 key=%s", key))
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeySuccess)
}

// invalidLoginCaptchaResult 统一构造登录验证码无效响应。
func invalidLoginCaptchaResult(err error) *types.BizResult {
	return types.NewBizResult(codes.InvalidCaptcha).
		SetI18nMessage(i18n.MsgKeyInvalidCaptcha).
		WithError(err)
}

// generateLoginCaptchaCode 生成指定长度的验证码文本。
func generateLoginCaptchaCode(length int) (string, error) {
	if length <= 0 {
		length = loginCaptchaLength
	}
	builder := strings.Builder{}
	for range length {
		index, err := randomInt(len(loginCaptchaAlphabet))
		if err != nil {
			return "", errors.Tag(err)
		}
		builder.WriteRune(loginCaptchaAlphabet[index])
	}
	return builder.String(), nil
}

// buildLoginCaptchaSVGDataURL 生成包含简单干扰线的 SVG 验证码图片。
func buildLoginCaptchaSVGDataURL(code string) string {
	const (
		width  = 132
		height = 44
	)
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`, width, height, width, height))
	builder.WriteString(`<rect width="100%" height="100%" rx="8" fill="#f8fafc"/>`)
	for i := 0; i < 6; i++ {
		x1, _ := randomInt(width)
		y1, _ := randomInt(height)
		x2, _ := randomInt(width)
		y2, _ := randomInt(height)
		builder.WriteString(fmt.Sprintf(`<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#cbd5e1" stroke-width="1"/>`, x1, y1, x2, y2))
	}
	for i := 0; i < len(code); i++ {
		rotate, _ := randomInt(31)
		offsetY, _ := randomInt(7)
		colorIdx, _ := randomInt(4)
		colors := []string{"#1d4ed8", "#0f172a", "#7c3aed", "#047857"}
		builder.WriteString(fmt.Sprintf(
			`<text x="%d" y="%d" font-size="26" font-family="Verdana,Arial,sans-serif" font-weight="700" fill="%s" transform="rotate(%d %d %d)">%s</text>`,
			18+i*26,
			31+offsetY,
			colors[colorIdx],
			rotate-15,
			18+i*26,
			31+offsetY,
			html.EscapeString(string(code[i])),
		))
	}
	builder.WriteString(`</svg>`)
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(builder.String()))
}

// randomInt 生成 [0, max) 的安全随机整数。
func randomInt(max int) (int, error) {
	if max <= 0 {
		return 0, errors.New("max 必须大于 0")
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0, errors.Tag(err)
	}
	return int(n.Int64()), nil
}
