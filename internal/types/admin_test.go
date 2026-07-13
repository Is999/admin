package types

import "testing"

// TestAdminSessionUsesLowerCamelLoginAddr 验证会话缓存只使用标准小驼峰字段名。
func TestAdminSessionUsesLowerCamelLoginAddr(t *testing.T) {
	session := &AdminSession{
		LastLoginIPAddr: "中国香港",
	}

	values := session.ToMap()
	if _, ok := values["LastLoginIPAddr"]; ok {
		t.Fatal("登录态缓存不应写入 Go 结构体字段名 LastLoginIPAddr")
	}
	if got := values["lastLoginIpaddr"]; got != "中国香港" {
		t.Fatalf("lastLoginIpaddr = %v, want 中国香港", got)
	}

	var restored AdminSession
	if err := restored.FromMap(map[string]string{"lastLoginIpaddr": "中国香港"}); err != nil {
		t.Fatalf("FromMap() error = %v", err)
	}
	if restored.LastLoginIPAddr != "中国香港" {
		t.Fatalf("LastLoginIPAddr = %q, want 中国香港", restored.LastLoginIPAddr)
	}
}

// TestAdminRoleAssignReqAllowsEmptyRoles 校验覆盖保存角色时空数组表示撤销全部角色。
func TestAdminRoleAssignReqAllowsEmptyRoles(t *testing.T) {
	req := &AdminRoleAssignReq{ID: 7, RoleIDs: []int{}}
	if err := req.Validate(); err != nil {
		t.Fatalf("AdminRoleAssignReq.Validate() error = %v, want nil", err)
	}
}

// TestAdminRoleRequestsRejectInvalidRoleID 校验新增和覆盖保存都拒绝非正角色 ID。
func TestAdminRoleRequestsRejectInvalidRoleID(t *testing.T) {
	if err := (&AdminRoleAssignReq{ID: 7, RoleIDs: []int{0}}).Validate(); err == nil {
		t.Fatal("AdminRoleAssignReq.Validate() error = nil, want invalid role ID")
	}
	if err := (&AddAdminReq{
		Username: "operator",
		Password: "Codex#123456",
		RoleIDs:  []int{-1},
	}).Validate(); err == nil {
		t.Fatal("AddAdminReq.Validate() error = nil, want invalid role ID")
	}
}

// TestRolePermissionSaveReqRejectsInvalidPermissionIDs 验证角色授权不会把非法 ID 静默过滤成撤销授权。
func TestRolePermissionSaveReqRejectsInvalidPermissionIDs(t *testing.T) {
	tests := []RolePermissionSaveReq{
		{ID: 1, RoutePermissionIDs: []int{0}},
		{ID: 1, DocPermissionIDs: []int{-1}},
	}
	for _, req := range tests {
		if err := req.Validate(); err == nil {
			t.Fatalf("RolePermissionSaveReq.Validate() error = nil, req=%+v", req)
		}
	}
}

// TestRolePermissionSaveReqRequiresBothExplicitLists 验证省略任一权限列表不会被解释为撤销全部权限。
func TestRolePermissionSaveReqRequiresBothExplicitLists(t *testing.T) {
	if err := (&RolePermissionSaveReq{ID: 1, DocPermissionIDs: []int{}}).Validate(); err == nil {
		t.Fatal("RolePermissionSaveReq.Validate() error = nil, want missing route list")
	}
	if err := (&RolePermissionSaveReq{ID: 1, RoutePermissionIDs: []int{}}).Validate(); err == nil {
		t.Fatal("RolePermissionSaveReq.Validate() error = nil, want missing document list")
	}
	if err := (&RolePermissionSaveReq{ID: 1, RoutePermissionIDs: []int{}, DocPermissionIDs: []int{}}).Validate(); err != nil {
		t.Fatalf("RolePermissionSaveReq.Validate() error = %v, want explicit revoke-all", err)
	}
}

// TestAdminRoleAssignReqRequiresExplicitList 验证漏传角色列表不会被解释为撤销全部角色。
func TestAdminRoleAssignReqRequiresExplicitList(t *testing.T) {
	if err := (&AdminRoleAssignReq{ID: 1}).Validate(); err == nil {
		t.Fatal("AdminRoleAssignReq.Validate() error = nil, want missing role list")
	}
	if err := (&AdminRoleAssignReq{ID: 1, RoleIDs: []int{}}).Validate(); err != nil {
		t.Fatalf("AdminRoleAssignReq.Validate() error = %v, want explicit revoke-all", err)
	}
}

// TestUpdateRBACRequestsRequireExplicitMutableFields 验证编辑契约不会把漏传字段静默解释成清空或根节点。
func TestUpdateRBACRequestsRequireExplicitMutableFields(t *testing.T) {
	if err := (&UpdateRoleReq{ID: 2, Title: "运营"}).Validate(); err == nil {
		t.Fatal("UpdateRoleReq.Validate() error = nil, want missing pid")
	}
	pid := 0
	if err := (&UpdatePermissionReq{ID: 2, Title: "列表", Pid: &pid}).Validate(); err == nil {
		t.Fatal("UpdatePermissionReq.Validate() error = nil, want missing type")
	}
	permissionType := 0
	if err := (&UpdatePermissionReq{ID: 2, Title: "列表", Pid: &pid, Type: &permissionType}).Validate(); err == nil {
		t.Fatal("UpdatePermissionReq.Validate() error = nil, want missing module and description")
	}
	empty := ""
	if err := (&UpdateRoleReq{ID: 2, Title: "运营", Pid: &pid, Description: &empty}).Validate(); err != nil {
		t.Fatalf("UpdateRoleReq.Validate() error = %v, want explicit empty description", err)
	}
	if err := (&UpdatePermissionReq{
		ID:          2,
		Title:       "列表",
		Pid:         &pid,
		Type:        &permissionType,
		Module:      &empty,
		Description: &empty,
	}).Validate(); err != nil {
		t.Fatalf("UpdatePermissionReq.Validate() error = %v, want explicit empty module and description", err)
	}
}
