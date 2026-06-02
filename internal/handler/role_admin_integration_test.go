package handler_test

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	codes "admin/common/codes"
	"admin/internal/bootstrap"
	secretkeylogic "admin/internal/logic/secretkey"
	securitylogic "admin/internal/logic/security"
	"admin/internal/security"
	"admin/internal/svc"
)

// roleAdminIntegrationResp 表示接口统一业务响应结构，便于集成测试解码 code/message/data。
type roleAdminIntegrationResp struct {
	Code    int             `json:"code"`    // Code 表示响应业务码。
	Message string          `json:"message"` // Message 表示响应消息。
	Data    json.RawMessage `json:"data"`    // Data 表示响应数据。
}

// roleAdminIntegrationLoginResp 表示登录返回的令牌结构。
type roleAdminIntegrationLoginResp struct {
	Token string `json:"token"` // Token 表示登录令牌。
}

// roleAdminIntegrationCaptchaResp 表示图形验证码返回结构。
type roleAdminIntegrationCaptchaResp struct {
	Key   string `json:"key"`   // Key 表示测试 key。
	Image string `json:"image"` // Image 表示验证码图片。
}

// roleAdminIntegrationPermissionItem 表示权限树节点结构。
type roleAdminIntegrationPermissionItem struct {
	ID       int                                  `json:"id"`       // ID 表示测试记录 ID。
	UUID     string                               `json:"uuid"`     // UUID 表示测试 UUID。
	Module   string                               `json:"module"`   // Module 表示权限模块。
	Status   int                                  `json:"status"`   // Status 表示状态值。
	Checked  bool                                 `json:"checked"`  // Checked 表示权限是否选中。
	Children []roleAdminIntegrationPermissionItem `json:"children"` // Children 表示子节点列表。
}

// roleAdminIntegrationRoleItem 表示角色树节点结构。
type roleAdminIntegrationRoleItem struct {
	ID          int                            `json:"id"`          // ID 表示测试记录 ID。
	Title       string                         `json:"title"`       // Title 表示标题。
	Status      int                            `json:"status"`      // Status 表示状态值。
	Permissions []int                          `json:"permissions"` // Permissions 表示权限列表。
	Children    []roleAdminIntegrationRoleItem `json:"children"`    // Children 表示子节点列表。
}

// roleAdminIntegrationAdminRoleItem 表示管理员已绑定角色项。
type roleAdminIntegrationAdminRoleItem struct {
	ID     int    `json:"id"`     // ID 表示测试记录 ID。
	RoleID int    `json:"roleID"` // RoleID 表示角色 ID。
	Title  string `json:"title"`  // Title 表示标题。
}

// roleAdminIntegrationAdminItem 表示管理员列表项。
type roleAdminIntegrationAdminItem struct {
	ID       int    `json:"id"`       // ID 表示测试记录 ID。
	Username string `json:"username"` // Username 表示用户名。
}

const (
	// integrationAppID 表示测试使用的常量。
	integrationAppID = "1"
	// integrationSignatureMD5 表示测试使用的常量。
	integrationSignatureMD5 = "M"
)

// TestRoleAdminIntegrationFlows 验证登录、角色父子权限收敛、状态切换和管理员角色绑定过滤链路。
func TestRoleAdminIntegrationFlows(t *testing.T) {
	configFile := "../../etc/config.dnmp.runtime.sample.yaml"
	if _, err := os.Stat(configFile); err != nil {
		t.Skipf("运行时配置不存在，跳过集成测试: %v", err)
	}

	cfg, err := bootstrap.LoadConfig(configFile)
	if err != nil {
		t.Fatalf("读取运行时配置失败: %v", err)
	}
	cfg.Host = "127.0.0.1"
	cfg.Port = integrationFreePort(t)
	cfg.Task.Enabled = false
	cfg.HotReload.Enabled = false
	cfg.Observability.TraceEnabled = false

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app, err := bootstrap.New(ctx, cfg, bootstrap.ModeAPI)
	if err != nil {
		t.Skipf("运行时依赖未就绪，跳过集成测试: %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		if stopErr := app.Stop(stopCtx); stopErr != nil {
			t.Fatalf("停止应用失败: %v", stopErr)
		}
	}()

	startErrCh := make(chan error, 1)
	go func() {
		startErrCh <- app.Start()
	}()

	baseURL := fmt.Sprintf("http://%s:%d", cfg.Host, cfg.Port)
	integrationWaitForServer(t, baseURL, startErrCh)

	client := &http.Client{Timeout: 20 * time.Second}
	testPassword := strings.TrimSpace(os.Getenv("CRON_ADMIN_TEST_PASSWORD"))
	if testPassword == "" {
		t.Skip("未设置 CRON_ADMIN_TEST_PASSWORD，跳过需要真实登录口令的集成测试")
	}
	superToken := integrationLogin(t, client, baseURL, app.ServiceContext, "super999", testPassword)

	// 先验证登录后初始化和权限码接口可正常返回，确保前端初始化链路可用。
	afterInfoResp := integrationDo(t, client, http.MethodGet, baseURL+"/api/auth/profile", "auth.profile", superToken, nil)
	if afterInfoResp.Code == codes.CheckMFACode || afterInfoResp.Code == codes.CheckMFABind || afterInfoResp.Code == codes.CheckMFAAgain {
		t.Skipf("当前环境登录态要求 MFA 校验，集成测试无法自动完成 MFA，跳过: %s", afterInfoResp.Message)
	}
	if !codes.IsSuccess(afterInfoResp.Code) {
		t.Fatalf("接口返回失败: method=%s url=%s code=%d message=%s", http.MethodGet, baseURL+"/api/auth/profile", afterInfoResp.Code, afterInfoResp.Message)
	}
	integrationMustDo(t, client, http.MethodGet, baseURL+"/api/auth/codes", "auth.codes", superToken, nil, nil)

	var permissionTree []roleAdminIntegrationPermissionItem
	integrationMustDo(t, client, http.MethodGet, baseURL+"/api/permissions/tree", "permission.tree.list", superToken, nil, &permissionTree)

	parentPermissionIDs := integrationPickPermissionIDs(t, permissionTree, "role.add", "role.update", "role.status.update")
	childPermissionIDs := append([]int(nil), parentPermissionIDs[:2]...)
	removedPermissionID := childPermissionIDs[1]

	suffix := time.Now().UnixNano()
	parentTitle := fmt.Sprintf("自动化父角色-%d", suffix)
	childTitle := fmt.Sprintf("自动化子角色-%d", suffix)
	adminUsername := fmt.Sprintf("ar%d", suffix%1_000_000_000)

	integrationMustDo(t, client, http.MethodPost, baseURL+"/api/roles", "role.add", superToken, map[string]any{
		"title":       parentTitle,
		"pid":         0,
		"description": "集成测试父角色",
		"permissions": parentPermissionIDs,
	}, nil)

	var roleTree []roleAdminIntegrationRoleItem
	integrationMustDo(t, client, http.MethodGet, baseURL+"/api/roles/tree", "role.tree.list", superToken, nil, &roleTree)
	parentRole := integrationFindRoleByTitle(t, roleTree, parentTitle)

	integrationMustDo(t, client, http.MethodPost, fmt.Sprintf("%s/api/roles", baseURL), "role.add", superToken, map[string]any{
		"title":       childTitle,
		"pid":         parentRole.ID,
		"description": "集成测试子角色",
		"permissions": childPermissionIDs,
	}, nil)

	integrationMustDo(t, client, http.MethodGet, baseURL+"/api/roles/tree", "role.tree.list", superToken, nil, &roleTree)
	parentRole = integrationFindRoleByTitle(t, roleTree, parentTitle)
	childRole := integrationFindRoleByTitle(t, roleTree, childTitle)
	parentCheckedPermissionIDs := integrationGetCheckedPermissionIDs(t, client, baseURL, superToken, parentRole.ID)
	childCheckedPermissionIDs := integrationGetCheckedPermissionIDs(t, client, baseURL, superToken, childRole.ID)
	if !slices.Equal(parentCheckedPermissionIDs, parentPermissionIDs) {
		t.Fatalf("父角色初始权限不符合预期: got=%v want=%v", parentCheckedPermissionIDs, parentPermissionIDs)
	}
	if !slices.Equal(childCheckedPermissionIDs, childPermissionIDs) {
		t.Fatalf("子角色初始权限不符合预期: got=%v want=%v", childCheckedPermissionIDs, childPermissionIDs)
	}

	// 编辑父角色时移除一个子角色也持有的权限，校验子角色越权权限会被同步清理。
	updatedParentPermissionIDs := []int{parentPermissionIDs[0], parentPermissionIDs[2]}
	integrationMustDo(t, client, http.MethodPatch, fmt.Sprintf("%s/api/roles/%d", baseURL, parentRole.ID), "role.update", superToken, map[string]any{
		"title":       parentTitle,
		"pid":         0,
		"description": "集成测试父角色-更新",
		"permissions": updatedParentPermissionIDs,
		"status":      1,
	}, nil)

	integrationMustDo(t, client, http.MethodGet, baseURL+"/api/roles/tree", "role.tree.list", superToken, nil, &roleTree)
	parentRole = integrationFindRoleByTitle(t, roleTree, parentTitle)
	childRole = integrationFindRoleByTitle(t, roleTree, childTitle)
	childCheckedPermissionIDs = integrationGetCheckedPermissionIDs(t, client, baseURL, superToken, childRole.ID)
	if slices.Contains(childCheckedPermissionIDs, removedPermissionID) {
		t.Fatalf("父角色移除权限后，子角色仍保留越权权限: %d", removedPermissionID)
	}

	// 创建管理员并同时提交父子角色，后端应自动过滤子角色，仅保留父角色绑定。
	addUserTwoStep := integrationIssueMFATwoStep(t, app.ServiceContext, 1, securitylogic.MFAScenarioAddUser)
	integrationMustDo(t, client, http.MethodPost, baseURL+"/api/admins", "admin.add", superToken, map[string]any{
		"username":     adminUsername,
		"realName":     "集成测试管理员",
		"password":     "PassWord3!",
		"email":        fmt.Sprintf("%s@example.com", adminUsername),
		"phone":        "13800138000",
		"avatar":       "",
		"description":  "集成测试管理员",
		"roleIDs":      []int{parentRole.ID, childRole.ID},
		"twoStepKey":   addUserTwoStep.Key,
		"twoStepValue": addUserTwoStep.Value,
	}, nil)

	var adminList struct {
		List []roleAdminIntegrationAdminItem `json:"list"` // List 表示列表数据。
	}
	integrationMustDo(t, client, http.MethodGet, fmt.Sprintf("%s/api/admins?username=%s", baseURL, url.QueryEscape(adminUsername)), "admin.list", superToken, nil, &adminList)
	if len(adminList.List) != 1 {
		t.Fatalf("按用户名查询管理员失败: got=%d want=1", len(adminList.List))
	}
	adminID := adminList.List[0].ID

	var adminRoles []roleAdminIntegrationAdminRoleItem
	integrationMustDo(t, client, http.MethodGet, fmt.Sprintf("%s/api/admins/roles/%d", baseURL, adminID), "admin.role.list", superToken, nil, &adminRoles)
	if len(adminRoles) != 1 {
		t.Fatalf("管理员绑定角色过滤失败: got=%d want=1", len(adminRoles))
	}
	if adminRoles[0].ID != parentRole.ID && adminRoles[0].RoleID != parentRole.ID {
		t.Fatalf("管理员最终绑定角色不是父角色: %+v", adminRoles[0])
	}

	// 最后用新管理员重新登录并验证权限码接口能成功返回，确认前后端初始化链路未被破坏。
	adminToken := integrationLogin(t, client, baseURL, app.ServiceContext, adminUsername, "PassWord3!")
	integrationMustDo(t, client, http.MethodGet, baseURL+"/api/auth/profile", "auth.profile", adminToken, nil, nil)
	integrationMustDo(t, client, http.MethodGet, baseURL+"/api/auth/codes", "auth.codes", adminToken, nil, nil)

	// 最后再验证禁用角色接口和列表状态回写，避免影响前面的“管理员绑定角色”校验。
	integrationMustDo(t, client, http.MethodPatch, fmt.Sprintf("%s/api/roles/status/%d", baseURL, childRole.ID), "role.status.update", superToken, map[string]any{
		"status": 0,
	}, nil)

	integrationMustDo(t, client, http.MethodGet, baseURL+"/api/roles/tree", "role.tree.list", superToken, nil, &roleTree)
	childRole = integrationFindRoleByTitle(t, roleTree, childTitle)
	if childRole.Status != 0 {
		t.Fatalf("子角色状态更新失败: got=%d want=0", childRole.Status)
	}

	// 清理集成测试创建的数据，避免污染后续人工验证。
	integrationMustDo(t, client, http.MethodDelete, fmt.Sprintf("%s/api/admins/%d", baseURL, adminID), "admin.delete", superToken, nil, nil)
	integrationMustDo(t, client, http.MethodDelete, fmt.Sprintf("%s/api/roles/%d", baseURL, childRole.ID), "role.delete", superToken, nil, nil)
	integrationMustDo(t, client, http.MethodDelete, fmt.Sprintf("%s/api/roles/%d", baseURL, parentRole.ID), "role.delete", superToken, nil, nil)
}

// integrationFreePort 申请一个本地空闲端口，避免和开发中的服务端口冲突。
func integrationFreePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("申请空闲端口失败: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

// integrationWaitForServer 轮询等待测试服务启动成功。
func integrationWaitForServer(t *testing.T, baseURL string, startErrCh <-chan error) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-startErrCh:
			if err != nil {
				t.Fatalf("启动测试服务失败: %v", err)
			}
			t.Fatalf("测试服务过早退出")
		default:
		}

		resp, err := client.Get(baseURL + "/health")
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("等待测试服务启动超时: %s", baseURL)
}

// integrationMFATwoStepTicket 表示测试里直接签发的 MFA 二次校验票据。
type integrationMFATwoStepTicket struct {
	Key   string // Key 表示测试 key。
	Value string // Value 表示字段值。
}

// integrationIssueMFATwoStep 为指定管理员直接签发指定场景的 MFA 二次票据，避免集成测试依赖固定 TOTP 秘钥。
func integrationIssueMFATwoStep(t *testing.T, svcCtx *svc.ServiceContext, adminID int, scenario int) integrationMFATwoStepTicket {
	t.Helper()
	if svcCtx == nil {
		t.Fatalf("签发 MFA 二次票据失败: service context 为空")
	}
	twoStep, err := securitylogic.NewSecurityLogic(context.Background(), svcCtx).IssueMFATwoStepTicket(adminID, scenario)
	if err != nil {
		t.Fatalf("签发 MFA 二次票据失败: adminID=%d scenario=%d err=%v", adminID, scenario, err)
	}
	if twoStep == nil || strings.TrimSpace(twoStep.Key) == "" || strings.TrimSpace(twoStep.Value) == "" {
		t.Fatalf("签发 MFA 二次票据返回为空: adminID=%d scenario=%d resp=%+v", adminID, scenario, twoStep)
	}
	return integrationMFATwoStepTicket{
		Key:   twoStep.Key,
		Value: twoStep.Value,
	}
}

// integrationLogin 通过验证码登录接口获取访问令牌。
// 注意：auth.login 响应可能包含加密后的 token 字段，集成测试需要在本地解密后再作为 Bearer token 使用。
func integrationLogin(t *testing.T, client *http.Client, baseURL string, svcCtx *svc.ServiceContext, username string, password string) string {
	t.Helper()

	var captcha roleAdminIntegrationCaptchaResp
	integrationMustDo(t, client, http.MethodGet, baseURL+"/api/auth/captcha", "auth.captcha", "", nil, &captcha)
	svg := integrationDecodeCaptchaSVG(t, captcha.Image)
	code := integrationExtractCaptchaCode(t, svg)

	var loginResp roleAdminIntegrationLoginResp
	integrationMustDo(t, client, http.MethodPost, baseURL+"/api/auth/login", "auth.login", "", map[string]any{
		"username": username,
		"password": password,
		"key":      captcha.Key,
		"captcha":  code,
	}, &loginResp)
	if strings.TrimSpace(loginResp.Token) == "" {
		t.Fatalf("登录成功但返回 token 为空: username=%s", username)
	}
	return integrationNormalizeBearerToken(t, svcCtx, loginResp.Token)
}

// integrationNormalizeBearerToken 把登录响应中的 token 统一转换为可直接用于 Authorization 的 JWT。
// 当 token 已经是 JWT 时直接返回；否则按当前默认加密算法（AES）解密。
func integrationNormalizeBearerToken(t *testing.T, svcCtx *svc.ServiceContext, token string) string {
	t.Helper()

	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if integrationLooksLikeJWT(token) {
		return token
	}
	if svcCtx == nil {
		t.Fatalf("登录 token 需要解密但 ServiceContext 为空")
	}
	aesKey, _, err := secretkeylogic.NewSecretKeyLogic(context.Background(), svcCtx).GetAESKey(integrationAppID, "", "")
	if err != nil || aesKey == nil {
		t.Fatalf("读取 AES Key 失败: %v", err)
	}
	cryptor, err := security.NewAESCipher(aesKey.Key, aesKey.IV)
	if err != nil {
		t.Fatalf("初始化 AES 解密器失败: %v", err)
	}
	plain, err := cryptor.Decrypt(token)
	if err != nil {
		t.Fatalf("登录 token 解密失败: %v", err)
	}
	if !integrationLooksLikeJWT(plain) {
		t.Fatalf("登录 token 解密后仍不是 JWT: %s", plain)
	}
	return plain
}

// integrationLooksLikeJWT 判断字符串是否形如 `header.payload.signature` 的 JWT 结构。
func integrationLooksLikeJWT(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	return parts[0] != "" && parts[1] != "" && parts[2] != ""
}

// integrationShouldSign 返回集成测试辅助数据。
func integrationShouldSign(alias string) bool {
	alias = strings.TrimSpace(alias)
	if alias == "" || strings.EqualFold(alias, "ignore") {
		return false
	}
	policy := security.PolicyByRoute(alias)
	return len(policy.RequestSign) > 0 || len(policy.ResponseSign) > 0
}

// integrationAppHeader 返回集成测试辅助数据。
func integrationAppHeader() string {
	return base64.StdEncoding.EncodeToString([]byte(integrationAppID))
}

// integrationTraceID 返回集成测试辅助数据。
func integrationTraceID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// integrationTimestamp 返回集成测试辅助数据。
func integrationTimestamp() string {
	return fmt.Sprintf("%d", time.Now().Unix())
}

// integrationSignValue 返回集成测试辅助数据。
func integrationSignValue(signText string) string {
	sum := md5.Sum([]byte(signText))
	return hex.EncodeToString(sum[:])
}

// integrationAttachSignature 返回集成测试辅助数据。
func integrationAttachSignature(alias string, payload map[string]any, traceID string, timestamp string) map[string]any {
	next := make(map[string]any, len(payload)+1)
	for k, v := range payload {
		next[k] = v
	}
	policy := security.PolicyByRoute(alias)
	signText := security.BuildSignString(next, policy.RequestSign, traceID, timestamp, integrationAppID)
	next["sign"] = integrationSignValue(signText)
	return next
}

// integrationDecodeCaptchaSVG 解析 data url 中的 base64 SVG。
func integrationDecodeCaptchaSVG(t *testing.T, image string) string {
	t.Helper()
	parts := strings.SplitN(strings.TrimSpace(image), ",", 2)
	if len(parts) != 2 {
		t.Fatalf("验证码图片格式不合法: %s", image)
	}
	raw, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("解码验证码图片失败: %v", err)
	}
	return string(raw)
}

// integrationExtractCaptchaCode 从 SVG 文本节点中抽取验证码内容。
func integrationExtractCaptchaCode(t *testing.T, svg string) string {
	t.Helper()
	matches := regexp.MustCompile(`<text[^>]*>([^<]+)</text>`).FindAllStringSubmatch(svg, -1)
	if len(matches) == 0 {
		t.Fatalf("验证码 SVG 中未找到文本节点: %s", svg)
	}
	var builder strings.Builder
	for _, match := range matches {
		builder.WriteString(match[1])
	}
	code := strings.TrimSpace(builder.String())
	if code == "" {
		t.Fatalf("验证码内容为空: %s", svg)
	}
	return code
}

// integrationMustDo 发起一次接口请求，并断言业务响应为成功。
func integrationMustDo(t *testing.T, client *http.Client, method string, urlText string, alias string, token string, payload any, out any) {
	t.Helper()
	signEnabled := integrationShouldSign(alias)
	traceID := ""
	timestamp := ""

	payloadMap := map[string]any{}
	if payload != nil {
		candidate, ok := payload.(map[string]any)
		if !ok {
			t.Fatalf("请求参数必须是 map[string]any: got=%T", payload)
		}
		for k, v := range candidate {
			payloadMap[k] = v
		}
	}

	parsedURL, err := url.Parse(urlText)
	if err != nil {
		t.Fatalf("解析请求地址失败: %v", err)
	}

	queryCarrier := method == http.MethodGet || method == http.MethodDelete
	queryParams := parsedURL.Query()
	signParams := map[string]any{}
	for k, values := range queryParams {
		if len(values) == 0 {
			continue
		}
		signParams[k] = values[len(values)-1]
	}
	if queryCarrier {
		for k, v := range payloadMap {
			queryParams.Set(k, fmt.Sprint(v))
			signParams[k] = v
		}
	}
	if signEnabled {
		traceID = integrationTraceID()
		timestamp = integrationTimestamp()
		// GET/DELETE 走 query 参数承载签名；POST/PUT/PATCH 走 body 承载签名。
		// 这里必须使用“最终待提交的业务参数”参与签名，避免出现“签名只覆盖空参数”的误判。
		if queryCarrier {
			signed := integrationAttachSignature(alias, signParams, traceID, timestamp)
			for k, v := range signed {
				queryParams.Set(k, fmt.Sprint(v))
			}
		} else {
			payloadMap = integrationAttachSignature(alias, payloadMap, traceID, timestamp)
		}
	}
	parsedURL.RawQuery = queryParams.Encode()

	var bodyReader io.Reader
	if queryCarrier {
		bodyReader = bytes.NewReader(nil)
	} else {
		if payload == nil {
			bodyReader = bytes.NewReader(nil)
		} else {
			raw, err := json.Marshal(payloadMap)
			if err != nil {
				t.Fatalf("序列化请求参数失败: %v", err)
			}
			bodyReader = bytes.NewReader(raw)
		}
	}

	req, err := http.NewRequest(method, parsedURL.String(), bodyReader)
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if signEnabled {
		req.Header.Set("X-App-Id", integrationAppHeader())
		req.Header.Set("X-Trace-Id", traceID)
		req.Header.Set("X-Timestamp", timestamp)
		req.Header.Set("X-Signature", integrationSignatureMD5)
	}
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("调用接口失败: method=%s url=%s err=%v", method, parsedURL.String(), err)
	}
	defer resp.Body.Close()
	t.Logf("api call: method=%s alias=%s path=%s status=%d latency=%s", method, alias, parsedURL.Path, resp.StatusCode, time.Since(start))

	var bizResp roleAdminIntegrationResp
	if err = json.NewDecoder(resp.Body).Decode(&bizResp); err != nil {
		t.Fatalf("解析响应失败: method=%s url=%s err=%v", method, parsedURL.String(), err)
	}
	if !codes.IsSuccess(bizResp.Code) {
		t.Fatalf("接口返回失败: method=%s url=%s code=%d message=%s", method, parsedURL.String(), bizResp.Code, bizResp.Message)
	}
	if out == nil || len(bizResp.Data) == 0 || string(bizResp.Data) == "null" {
		return
	}
	if err = json.Unmarshal(bizResp.Data, out); err != nil {
		t.Fatalf("解析业务数据失败: method=%s url=%s err=%v data=%s", method, parsedURL.String(), err, string(bizResp.Data))
	}
}

// integrationDo 发起一次接口请求并返回业务响应结构。
// 该方法主要用于集成测试的“环境探测”场景：部分环境可能强制 MFA 校验或存在运维限流，
// 这类场景不适合直接用 integrationMustDo 做强断言。
func integrationDo(t *testing.T, client *http.Client, method string, urlText string, alias string, token string, payload any) roleAdminIntegrationResp {
	t.Helper()

	signEnabled := integrationShouldSign(alias)
	traceID := ""
	timestamp := ""

	payloadMap := map[string]any{}
	if payload != nil {
		candidate, ok := payload.(map[string]any)
		if !ok {
			t.Fatalf("请求参数必须是 map[string]any: got=%T", payload)
		}
		for k, v := range candidate {
			payloadMap[k] = v
		}
	}

	parsedURL, err := url.Parse(urlText)
	if err != nil {
		t.Fatalf("解析请求地址失败: %v", err)
	}

	queryCarrier := method == http.MethodGet || method == http.MethodDelete
	queryParams := parsedURL.Query()
	signParams := map[string]any{}
	for k, values := range queryParams {
		if len(values) == 0 {
			continue
		}
		signParams[k] = values[len(values)-1]
	}
	if queryCarrier {
		for k, v := range payloadMap {
			queryParams.Set(k, fmt.Sprint(v))
			signParams[k] = v
		}
	}
	if signEnabled {
		traceID = integrationTraceID()
		timestamp = integrationTimestamp()
		if queryCarrier {
			signed := integrationAttachSignature(alias, signParams, traceID, timestamp)
			for k, v := range signed {
				queryParams.Set(k, fmt.Sprint(v))
			}
		} else {
			payloadMap = integrationAttachSignature(alias, payloadMap, traceID, timestamp)
		}
	}
	parsedURL.RawQuery = queryParams.Encode()

	var bodyReader io.Reader
	if queryCarrier {
		bodyReader = bytes.NewReader(nil)
	} else {
		if payload == nil {
			bodyReader = bytes.NewReader(nil)
		} else {
			raw, err := json.Marshal(payloadMap)
			if err != nil {
				t.Fatalf("序列化请求参数失败: %v", err)
			}
			bodyReader = bytes.NewReader(raw)
		}
	}

	req, err := http.NewRequest(method, parsedURL.String(), bodyReader)
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if signEnabled {
		req.Header.Set("X-App-Id", integrationAppHeader())
		req.Header.Set("X-Trace-Id", traceID)
		req.Header.Set("X-Timestamp", timestamp)
		req.Header.Set("X-Signature", integrationSignatureMD5)
	}
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("调用接口失败: method=%s url=%s err=%v", method, parsedURL.String(), err)
	}
	defer resp.Body.Close()
	t.Logf("api call: method=%s alias=%s path=%s status=%d latency=%s", method, alias, parsedURL.Path, resp.StatusCode, time.Since(start))

	var bizResp roleAdminIntegrationResp
	if err = json.NewDecoder(resp.Body).Decode(&bizResp); err != nil {
		t.Fatalf("解析响应失败: method=%s url=%s err=%v", method, parsedURL.String(), err)
	}
	return bizResp
}

// integrationPickPermissionIDs 按模块名挑选测试所需的权限 ID。
func integrationPickPermissionIDs(t *testing.T, tree []roleAdminIntegrationPermissionItem, modules ...string) []int {
	t.Helper()
	moduleToID := make(map[string]int, len(modules))
	var walk func(items []roleAdminIntegrationPermissionItem)
	walk = func(items []roleAdminIntegrationPermissionItem) {
		for _, item := range items {
			if item.Status == 1 && item.Module != "" {
				moduleToID[item.Module] = item.ID
			}
			walk(item.Children)
		}
	}
	walk(tree)

	ids := make([]int, 0, len(modules))
	for _, module := range modules {
		id := moduleToID[module]
		if id <= 0 {
			t.Fatalf("权限树中未找到模块: %s", module)
		}
		ids = append(ids, id)
	}
	return ids
}

// integrationGetCheckedPermissionIDs 读取角色权限树里当前已勾选的权限 ID。
func integrationGetCheckedPermissionIDs(t *testing.T, client *http.Client, baseURL string, token string, roleID int) []int {
	t.Helper()
	var tree []roleAdminIntegrationPermissionItem
	integrationMustDo(t, client, http.MethodGet, fmt.Sprintf("%s/api/roles/permissions/tree/%d/n", baseURL, roleID), "role.permission.tree", token, nil, &tree)

	ids := make([]int, 0, 16)
	var walk func(items []roleAdminIntegrationPermissionItem)
	walk = func(items []roleAdminIntegrationPermissionItem) {
		for _, item := range items {
			if item.Checked {
				ids = append(ids, item.ID)
			}
			walk(item.Children)
		}
	}
	walk(tree)
	slices.Sort(ids)
	return ids
}

// integrationFindRoleByTitle 按标题在角色树中查找目标角色。
func integrationFindRoleByTitle(t *testing.T, tree []roleAdminIntegrationRoleItem, title string) roleAdminIntegrationRoleItem {
	t.Helper()
	var walk func(items []roleAdminIntegrationRoleItem) *roleAdminIntegrationRoleItem
	walk = func(items []roleAdminIntegrationRoleItem) *roleAdminIntegrationRoleItem {
		for _, item := range items {
			if item.Title == title {
				return new(item)
			}
			if matched := walk(item.Children); matched != nil {
				return matched
			}
		}
		return nil
	}
	matched := walk(tree)
	if matched == nil {
		t.Fatalf("角色树中未找到角色: %s", title)
	}
	return *matched
}
