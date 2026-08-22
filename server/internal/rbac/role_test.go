package rbac

import "testing"

func TestRolePermissions(t *testing.T) {
	cases := []struct {
		role Role
		perm Permission
		want bool
	}{
		{RoleAdmin, PermUserManage, true},
		{RoleAdmin, PermClusterWrite, true},
		{RoleUser, PermClusterWrite, true},
		{RoleUser, PermUserManage, false},
		{RoleUser, PermClusterManage, false},
		{RoleReadOnly, PermClusterRead, true},
		{RoleReadOnly, PermClusterWrite, false},
		{RoleReadOnly, PermUserManage, false},
	}

	for _, c := range cases {
		if got := c.role.Can(c.perm); got != c.want {
			t.Errorf("%s.Can(%s) = %v, want %v", c.role, c.perm, got, c.want)
		}
	}
}

func TestUnknownRoleGrantsNothing(t *testing.T) {
	rogue := Role("superadmin")
	for _, p := range []Permission{PermClusterRead, PermClusterWrite, PermUserManage} {
		if rogue.Can(p) {
			t.Errorf("unknown role granted %s; default must be deny", p)
		}
	}
	if _, err := ParseRole("superadmin"); err == nil {
		t.Error("ParseRole accepted an unknown role")
	}
}

func TestAdminRequiresMFAWhenEnforced(t *testing.T) {
	if !RoleAdmin.RequiresMFA(true) {
		t.Error("admin does not require MFA when enforcement is on")
	}
	if RoleUser.RequiresMFA(true) {
		t.Error("non-admin requires MFA")
	}
	if RoleAdmin.RequiresMFA(false) {
		t.Error("admin requires MFA when enforcement is off")
	}
}
