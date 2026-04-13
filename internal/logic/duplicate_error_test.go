package logic

import (
	"context"
	"testing"

	"admin_cron/common/codes"

	"github.com/Is999/go-utils/errors"
	drivermysql "github.com/go-sql-driver/mysql"
)

func TestDuplicateResultMessagesAreExplicit(t *testing.T) {
	cases := []struct {
		name string
		got  int
		want int
		msg  string
	}{
		{
			name: "admin",
			got:  adminNameAlreadyExistsResult("alice", errAdminNameAlreadyExists).Code,
			want: codes.UserAlreadyExists,
			msg:  adminNameAlreadyExistsResult("alice", errAdminNameAlreadyExists).ResolveMessage(context.Background()),
		},
		{
			name: "role",
			got:  roleTitleAlreadyExistsResult("运营", errRoleTitleAlreadyExists).Code,
			want: codes.AdminRoleAlreadyExists,
			msg:  roleTitleAlreadyExistsResult("运营", errRoleTitleAlreadyExists).ResolveMessage(context.Background()),
		},
		{
			name: "permission",
			got:  permissionUUIDAlreadyExistsResult("100001", errPermissionUUIDAlreadyExists).Code,
			want: codes.AdminPermissionAlreadyExists,
			msg:  permissionUUIDAlreadyExistsResult("100001", errPermissionUUIDAlreadyExists).ResolveMessage(context.Background()),
		},
	}
	wantMessages := map[string]string{
		"admin":      "用户[alice]已存在",
		"role":       "角色[运营]已存在",
		"permission": "权限标识[100001]已存在",
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Fatalf("%s code = %d, want %d", tc.name, tc.got, tc.want)
		}
		if tc.msg != wantMessages[tc.name] {
			t.Fatalf("%s message = %q, want %q", tc.name, tc.msg, wantMessages[tc.name])
		}
	}
}

func TestIsMySQLDuplicateEntryErrorDetectsWrappedDuplicate(t *testing.T) {
	duplicateErr := &drivermysql.MySQLError{Number: mysqlDuplicateEntryErrorNumber, Message: "Duplicate entry"}
	if !isMySQLDuplicateEntryError(errors.Wrap(duplicateErr, "create admin")) {
		t.Fatal("期望识别被包装的 MySQL duplicate entry 错误")
	}
	otherErr := &drivermysql.MySQLError{Number: 1213, Message: "Deadlock found"}
	if isMySQLDuplicateEntryError(errors.Wrap(otherErr, "create admin")) {
		t.Fatal("不应把非 duplicate entry 的 MySQL 错误识别为重复数据")
	}
}
