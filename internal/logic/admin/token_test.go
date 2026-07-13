package admin

import (
	"context"
	"testing"
	"time"

	"admin/internal/config"
	corelogic "admin/internal/logic"
	"admin/internal/svc"

	"github.com/golang-jwt/jwt/v4"
)

// TestGenerateJWTUsesConfiguredExpiry 验证 JWT 固定有效期来自统一配置。
func TestGenerateJWTUsesConfiguredExpiry(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{
		JwtSecret:    "test-jwt-secret",
		JwtExpiresIn: 90,
	}, svc.Dependencies{})
	logicObj := &AdminLogic{BaseLogic: corelogic.NewBaseLogicWithContext(context.Background(), svcCtx)}

	before := time.Now().Unix()
	tokenText, err := logicObj.generateJWT(7, "admin", "127.0.0.1")
	if err != nil {
		t.Fatalf("generateJWT() error = %v", err)
	}
	claims := jwt.MapClaims{}
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	if _, _, err = parser.ParseUnverified(tokenText, claims); err != nil {
		t.Fatalf("ParseUnverified() error = %v", err)
	}
	iat, iatOK := claims["iat"].(float64)
	exp, expOK := claims["exp"].(float64)
	if !iatOK || !expOK {
		t.Fatalf("JWT 时间字段类型不正确: %#v", claims)
	}
	if int64(exp-iat) != 90 {
		t.Fatalf("JWT 有效期 = %d, want 90", int64(exp-iat))
	}
	if int64(iat) < before || int64(iat) > time.Now().Unix() {
		t.Fatalf("JWT iat 不在签发窗口内: %v", iat)
	}
	if jti, ok := claims["jti"].(string); !ok || jti == "" {
		t.Fatalf("JWT jti 缺失: %#v", claims)
	}

	secondTokenText, err := logicObj.generateJWT(7, "admin", "127.0.0.1")
	if err != nil {
		t.Fatalf("second generateJWT() error = %v", err)
	}
	if secondTokenText == tokenText {
		t.Fatal("同一秒签发的 JWT 不应完全相同")
	}
}
