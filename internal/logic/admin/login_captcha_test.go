package admin

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image/png"
	"net/http"
	"strings"
	"testing"

	codes "admin/common/codes"
	keys "admin/common/rediskeys"
	"admin/internal/config"
	corelogic "admin/internal/logic"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// newTestAdminCaptchaLogic 创建仅包含 Redis 的登录验证码测试逻辑对象。
func newTestAdminCaptchaLogic(t *testing.T) (*AdminLogic, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	svcCtx := svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{Rds: client})
	return &AdminLogic{BaseLogic: corelogic.NewBaseLogicWithContext(context.Background(), svcCtx)}, server
}

// TestBuildLoginCaptchaAndVerify 验证登录验证码可成功生成、校验并在成功后立即失效。
func TestBuildLoginCaptchaAndVerify(t *testing.T) {
	logicObj, _ := newTestAdminCaptchaLogic(t)
	resp := logicObj.BuildLoginCaptcha()
	if resp == nil || resp.IsFailure() {
		t.Fatalf("BuildLoginCaptcha() = %#v, want success", resp)
	}
	data, ok := resp.Data.(*types.LoginCaptchaResp)
	if !ok || data == nil {
		t.Fatalf("BuildLoginCaptcha() data = %#v, want *types.LoginCaptchaResp", resp.Data)
	}
	if strings.TrimSpace(data.Key) == "" || strings.TrimSpace(data.Image) == "" {
		t.Fatalf("BuildLoginCaptcha() returned empty key or image: %#v", data)
	}
	parts := strings.SplitN(data.Image, ",", 2)
	if len(parts) != 2 || parts[0] != "data:image/png;base64" {
		t.Fatalf("验证码图片必须是 PNG data URL，实际前缀为 %q", parts[0])
	}
	imageBytes, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("解码验证码 PNG 失败: %v", err)
	}
	if contentType := http.DetectContentType(imageBytes); contentType != "image/png" {
		t.Fatalf("验证码图片类型=%q，期望 image/png", contentType)
	}
	config, err := png.DecodeConfig(bytes.NewReader(imageBytes))
	if err != nil {
		t.Fatalf("读取验证码 PNG 尺寸失败: %v", err)
	}
	if config.Width != loginCaptchaImageWidth || config.Height != loginCaptchaImageHeight {
		t.Fatalf("验证码图片尺寸=%dx%d，期望 %dx%d", config.Width, config.Height, loginCaptchaImageWidth, loginCaptchaImageHeight)
	}
	if bytes.Contains(imageBytes, []byte("<text")) || bytes.Contains(imageBytes, []byte("<svg")) {
		t.Fatal("验证码图片不应包含可直接解析的 SVG 明文节点")
	}
	cacheKey := logicObj.AppRedisKey(fmt.Sprintf(keys.LoginCaptcha, data.Key))
	code, err := logicObj.Redis().Get(context.Background(), cacheKey).Result()
	if err != nil {
		t.Fatalf("Get(%s) error = %v", cacheKey, err)
	}
	verifyResp := logicObj.VerifyLoginCaptcha(data.Key, code)
	if verifyResp == nil || verifyResp.IsFailure() {
		t.Fatalf("VerifyLoginCaptcha(success) = %#v, want success", verifyResp)
	}
	verifyAgainResp := logicObj.VerifyLoginCaptcha(data.Key, code)
	if verifyAgainResp == nil || verifyAgainResp.Code != codes.InvalidCaptcha {
		t.Fatalf("VerifyLoginCaptcha(reuse) = %#v, want invalid captcha", verifyAgainResp)
	}
}

// TestVerifyLoginCaptchaRejectsWrongCode 验证错误验证码会被拒绝，并消费掉当前验证码。
func TestVerifyLoginCaptchaRejectsWrongCode(t *testing.T) {
	logicObj, _ := newTestAdminCaptchaLogic(t)
	resp := logicObj.BuildLoginCaptcha()
	data := resp.Data.(*types.LoginCaptchaResp)
	verifyResp := logicObj.VerifyLoginCaptcha(data.Key, "WRONG")
	if verifyResp == nil || verifyResp.Code != codes.InvalidCaptcha {
		t.Fatalf("VerifyLoginCaptcha(wrong) = %#v, want invalid captcha", verifyResp)
	}
	cacheKey := logicObj.AppRedisKey(fmt.Sprintf(keys.LoginCaptcha, data.Key))
	if logicObj.Redis().Exists(context.Background(), cacheKey).Val() != 0 {
		t.Fatalf("VerifyLoginCaptcha(wrong) should consume captcha key %s", cacheKey)
	}
}

// TestDrawLoginCaptchaPNGKeepsTextInsideImage 验证宽字符不会贴边裁切。
func TestDrawLoginCaptchaPNGKeepsTextInsideImage(t *testing.T) {
	for range 20 {
		imageBytes, err := drawLoginCaptchaPNG("WMWM")
		if err != nil {
			t.Fatalf("drawLoginCaptchaPNG() error = %v", err)
		}
		captchaImage, err := png.Decode(bytes.NewReader(imageBytes))
		if err != nil {
			t.Fatalf("解码验证码 PNG 失败: %v", err)
		}
		bounds := captchaImage.Bounds()
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				red, green, blue, _ := captchaImage.At(x, y).RGBA()
				if !isLoginCaptchaTextPixel(red, green, blue) {
					continue
				}
				if x <= bounds.Min.X+1 || x >= bounds.Max.X-2 || y <= bounds.Min.Y+1 || y >= bounds.Max.Y-2 {
					t.Fatalf("验证码深色文字像素贴边: x=%d y=%d bounds=%v", x, y, bounds)
				}
			}
		}
	}
}

// isLoginCaptchaTextPixel 判断像素是否属于深色文字区域。
func isLoginCaptchaTextPixel(red uint32, green uint32, blue uint32) bool {
	return int(red>>8)+int(green>>8)+int(blue>>8) < 420
}
