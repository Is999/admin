package logic

import (
	"context"
	"fmt"
	"strings"
	"testing"

	codes "admin_cron/common/codes"
	keys "admin_cron/common/rediskeys"
	"admin_cron/internal/svc"
	"admin_cron/internal/types"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// newTestAdminCaptchaLogic 创建仅包含 Redis 的登录验证码测试逻辑对象。
func newTestAdminCaptchaLogic(t *testing.T) (*AdminLogic, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	svcCtx := &svc.ServiceContext{Rds: client}
	return &AdminLogic{BaseLogic: NewBaseLogicWithContext(context.Background(), svcCtx)}, server
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
	cacheKey := fmt.Sprintf(keys.LoginCaptcha, data.Key)
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
	cacheKey := fmt.Sprintf(keys.LoginCaptcha, data.Key)
	if logicObj.Redis().Exists(context.Background(), cacheKey).Val() != 0 {
		t.Fatalf("VerifyLoginCaptcha(wrong) should consume captcha key %s", cacheKey)
	}
}
