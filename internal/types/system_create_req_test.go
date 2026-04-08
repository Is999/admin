package types

import (
	"encoding/json"
	"testing"
)

// TestCreateRoleReqValidateDoesNotRequirePathID 确保新增角色请求不再依赖路径 ID。
func TestCreateRoleReqValidateDoesNotRequirePathID(t *testing.T) {
	req := &CreateRoleReq{
		Title:       "财务",
		Pid:         2,
		Description: "财务",
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("expected create role validate success, got %v", err)
	}
}

// TestCreateRoleReqValidateKeepsNormalizedFields 确保新增角色校验会把字段清洗结果回写到原请求。
func TestCreateRoleReqValidateKeepsNormalizedFields(t *testing.T) {
	req := &CreateRoleReq{
		Title:       "  财务  ",
		Description: "  财务角色  ",
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("expected create role validate success, got %v", err)
	}
	saveReq := req.ToSaveRoleReq()
	if req.Title != "财务" || saveReq.Title != "财务" {
		t.Fatalf("expected normalized role title, got req=%q save=%q", req.Title, saveReq.Title)
	}
	if req.Description != "财务角色" || saveReq.Description != "财务角色" {
		t.Fatalf("expected normalized role description, got req=%q save=%q", req.Description, saveReq.Description)
	}
}

// TestCreatePermissionReqValidateDoesNotRequirePathID 确保新增权限请求不再依赖路径 ID。
func TestCreatePermissionReqValidateDoesNotRequirePathID(t *testing.T) {
	req := &CreatePermissionReq{
		Title: "查看报表",
		Type:  1,
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("expected create permission validate success, got %v", err)
	}
}

// TestCreatePermissionReqValidateKeepsNormalizedFields 确保新增权限校验会把字段清洗结果回写到原请求。
func TestCreatePermissionReqValidateKeepsNormalizedFields(t *testing.T) {
	req := &CreatePermissionReq{
		UUID:        "  system.role.add  ",
		Title:       "  新增角色  ",
		Module:      "  system.role.add  ",
		Type:        1,
		Description: "  新增角色权限  ",
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("expected create permission validate success, got %v", err)
	}
	saveReq := req.ToSavePermissionReq()
	if req.UUID != "system.role.add" || saveReq.UUID != "system.role.add" {
		t.Fatalf("expected normalized permission uuid, got req=%q save=%q", req.UUID, saveReq.UUID)
	}
	if req.Title != "新增角色" || saveReq.Title != "新增角色" {
		t.Fatalf("expected normalized permission title, got req=%q save=%q", req.Title, saveReq.Title)
	}
	if req.Module != "system.role.add" || saveReq.Module != "system.role.add" {
		t.Fatalf("expected normalized permission module, got req=%q save=%q", req.Module, saveReq.Module)
	}
	if req.Description != "新增角色权限" || saveReq.Description != "新增角色权限" {
		t.Fatalf("expected normalized permission description, got req=%q save=%q", req.Description, saveReq.Description)
	}
}

// TestCreateSysConfigReqValidateDoesNotRequirePathID 确保新增系统配置请求不再依赖路径 ID。
func TestCreateSysConfigReqValidateDoesNotRequirePathID(t *testing.T) {
	req := &CreateSysConfigReq{
		UUID:  "finance.report",
		Title: "财务报表开关",
		Type:  1,
		Value: []byte(`"on"`),
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("expected create sys config validate success, got %v", err)
	}
}

// TestCreateSysConfigReqValidateKeepsNormalizedFields 确保新增系统配置校验会把字段清洗结果回写到原请求。
func TestCreateSysConfigReqValidateKeepsNormalizedFields(t *testing.T) {
	req := &CreateSysConfigReq{
		UUID:   "  finance.report  ",
		Title:  "  财务报表开关  ",
		Type:   1,
		Value:  json.RawMessage(`"on"`),
		Remark: "  运营开关  ",
		Page:   "  /finance/report  ",
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("expected create sys config validate success, got %v", err)
	}
	saveReq := req.ToSaveSysConfigReq()
	if req.UUID != "finance.report" || saveReq.UUID != "finance.report" {
		t.Fatalf("expected normalized config uuid, got req=%q save=%q", req.UUID, saveReq.UUID)
	}
	if req.Title != "财务报表开关" || saveReq.Title != "财务报表开关" {
		t.Fatalf("expected normalized config title, got req=%q save=%q", req.Title, saveReq.Title)
	}
	if req.Remark != "运营开关" || saveReq.Remark != "运营开关" {
		t.Fatalf("expected normalized config remark, got req=%q save=%q", req.Remark, saveReq.Remark)
	}
	if req.Page != "/finance/report" || saveReq.Page != "/finance/report" {
		t.Fatalf("expected normalized config page, got req=%q save=%q", req.Page, saveReq.Page)
	}
}

// TestSaveSysConfigReqValidateRequiresVersionOnUpdate 确保编辑字典配置时必须携带版本号。
func TestSaveSysConfigReqValidateRequiresVersionOnUpdate(t *testing.T) {
	req := &SaveSysConfigReq{
		ID:    1,
		UUID:  "finance.report",
		Title: "财务报表开关",
		Type:  3,
		Value: json.RawMessage(`"on"`),
	}
	if err := req.Validate(); err == nil {
		t.Fatalf("expected update sys config validate error when version missing")
	}
}

// TestSaveSysConfigReqValidateRejectsNegativeVersion 确保字典配置版本号不能为负数。
func TestSaveSysConfigReqValidateRejectsNegativeVersion(t *testing.T) {
	req := &SaveSysConfigReq{
		ID:      1,
		UUID:    "finance.report",
		Title:   "财务报表开关",
		Type:    3,
		Value:   json.RawMessage(`"on"`),
		Version: new(-1),
	}
	if err := req.Validate(); err == nil {
		t.Fatalf("expected update sys config validate error when version is negative")
	}
}

// TestSaveSysConfigReqParseScalarJSON 确保标量 JSON 配置值可被标准 JSON 解码和后续归一化正确接收。
func TestSaveSysConfigReqParseScalarJSON(t *testing.T) {
	body := `{"id":7,"type":6,"pid":6,"uuid":"adminMFACheckEnable","title":"Admin校验MFA设备验证码","page":"","remark":"demo","example":1,"value":1,"version":0}`
	var parsed SaveSysConfigReq
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("expected json unmarshal success, got %v", err)
	}

	valueRaw, err := parsed.ValueRawMessage()
	if err != nil {
		t.Fatalf("expected value normalize success, got %v", err)
	}
	exampleRaw, err := parsed.ExampleRawMessage()
	if err != nil {
		t.Fatalf("expected example normalize success, got %v", err)
	}
	if string(valueRaw) != "1" {
		t.Fatalf("value raw = %s, want 1", string(valueRaw))
	}
	if string(exampleRaw) != "1" {
		t.Fatalf("example raw = %s, want 1", string(exampleRaw))
	}
}

// TestSecretKeyValidateReqValidateKeepsNormalizedFields 确保秘钥预检校验会把字段清洗和默认稳定版本回写到原请求。
func TestSecretKeyValidateReqValidateKeepsNormalizedFields(t *testing.T) {
	req := &SecretKeyValidateReq{
		UUID:                   "  finance-app  ",
		Title:                  "  财务系统  ",
		KeyVersion:             "  v1  ",
		AESKeyRef:              "  /data/keys/aes.key  ",
		AESIVRef:               "  /data/keys/aes.iv  ",
		RSAPublicKeyUserRef:    "  /data/keys/user.pub  ",
		RSAPublicKeyServerRef:  "  /data/keys/server.pub  ",
		RSAPrivateKeyServerRef: "  /data/keys/server.key  ",
		Status:                 0,
		SignStatus:             0,
		CryptoStatus:           0,
		VersionStatus:          0,
		GrayVersion:            "  v2  ",
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("expected secret key validate request success, got %v", err)
	}
	saveReq := req.ToSaveSecretKeyReq()
	if req.UUID != "finance-app" || saveReq.UUID != "finance-app" {
		t.Fatalf("expected normalized secret key uuid, got req=%q save=%q", req.UUID, saveReq.UUID)
	}
	if req.KeyVersion != "v1" || saveReq.KeyVersion != "v1" {
		t.Fatalf("expected normalized key version, got req=%q save=%q", req.KeyVersion, saveReq.KeyVersion)
	}
	if req.StableVersion != "v1" || saveReq.StableVersion != "v1" {
		t.Fatalf("expected default stable version, got req=%q save=%q", req.StableVersion, saveReq.StableVersion)
	}
	if req.GrayVersion != "v2" || saveReq.GrayVersion != "v2" {
		t.Fatalf("expected normalized gray version, got req=%q save=%q", req.GrayVersion, saveReq.GrayVersion)
	}
	if req.AESKeyRef != "/data/keys/aes.key" || saveReq.AESKeyRef != "/data/keys/aes.key" {
		t.Fatalf("expected normalized AES key ref, got req=%q save=%q", req.AESKeyRef, saveReq.AESKeyRef)
	}
}

// TestCreateSecretKeyReqValidateDoesNotRequirePathID 确保新增秘钥请求不再依赖路径 ID。
func TestCreateSecretKeyReqValidateDoesNotRequirePathID(t *testing.T) {
	req := &CreateSecretKeyReq{
		UUID:          "finance-app",
		Title:         "财务系统",
		KeyVersion:    "v1",
		Status:        0,
		VersionStatus: 0,
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("expected create secret key validate success, got %v", err)
	}
}

// TestCreateSecretKeyReqValidateKeepsNormalizedFields 确保新增秘钥校验会把字段清洗和默认稳定版本回写到原请求。
func TestCreateSecretKeyReqValidateKeepsNormalizedFields(t *testing.T) {
	req := &CreateSecretKeyReq{
		UUID:                   "  finance-app  ",
		Title:                  "  财务系统  ",
		KeyVersion:             "  v1  ",
		AESKeyRef:              "  /data/keys/aes.key  ",
		AESIVRef:               "  /data/keys/aes.iv  ",
		RSAPublicKeyUserRef:    "  /data/keys/user.pub  ",
		RSAPublicKeyServerRef:  "  /data/keys/server.pub  ",
		RSAPrivateKeyServerRef: "  /data/keys/server.key  ",
		Status:                 0,
		SignStatus:             0,
		CryptoStatus:           0,
		VersionStatus:          0,
		GrayVersion:            "  v2  ",
		Remark:                 "  首版秘钥  ",
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("expected create secret key validate success, got %v", err)
	}
	saveReq := req.ToSaveSecretKeyReq()
	if req.UUID != "finance-app" || saveReq.UUID != "finance-app" {
		t.Fatalf("expected normalized secret key uuid, got req=%q save=%q", req.UUID, saveReq.UUID)
	}
	if req.Title != "财务系统" || saveReq.Title != "财务系统" {
		t.Fatalf("expected normalized secret key title, got req=%q save=%q", req.Title, saveReq.Title)
	}
	if req.KeyVersion != "v1" || saveReq.KeyVersion != "v1" {
		t.Fatalf("expected normalized key version, got req=%q save=%q", req.KeyVersion, saveReq.KeyVersion)
	}
	if req.StableVersion != "v1" || saveReq.StableVersion != "v1" {
		t.Fatalf("expected default stable version, got req=%q save=%q", req.StableVersion, saveReq.StableVersion)
	}
	if req.GrayVersion != "v2" || saveReq.GrayVersion != "v2" {
		t.Fatalf("expected normalized gray version, got req=%q save=%q", req.GrayVersion, saveReq.GrayVersion)
	}
	if req.Remark != "首版秘钥" || saveReq.Remark != "首版秘钥" {
		t.Fatalf("expected normalized remark, got req=%q save=%q", req.Remark, saveReq.Remark)
	}
}
