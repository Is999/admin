package rbac

import (
	"context"
	"testing"

	"admin/common/codes"
)

func TestRBACDuplicateResultMessages(t *testing.T) {
	cases := []struct {
		name string
		got  int
		want int
		msg  string
		text string
	}{
		{
			name: "role",
			got:  roleTitleAlreadyExistsResult("运营", errRoleTitleAlreadyExists).Code,
			want: codes.AdminRoleAlreadyExists,
			msg:  roleTitleAlreadyExistsResult("运营", errRoleTitleAlreadyExists).ResolveMessage(context.Background()),
			text: "角色[运营]已存在",
		},
		{
			name: "permission",
			got:  permissionUUIDAlreadyExistsResult("100001", errPermissionUUIDAlreadyExists).Code,
			want: codes.AdminPermissionAlreadyExists,
			msg:  permissionUUIDAlreadyExistsResult("100001", errPermissionUUIDAlreadyExists).ResolveMessage(context.Background()),
			text: "权限标识[100001]已存在",
		},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Fatalf("%s code = %d, want %d", tc.name, tc.got, tc.want)
		}
		if tc.msg != tc.text {
			t.Fatalf("%s message = %q, want %q", tc.name, tc.msg, tc.text)
		}
	}
}
