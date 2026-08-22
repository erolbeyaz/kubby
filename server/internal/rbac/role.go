package rbac

import "fmt"

// Role is the Kubby-level role. It is deliberately coarse: fine-grained access is
// expressed per cluster (Faz 3) rather than by multiplying roles.
type Role string

const (
	RoleAdmin    Role = "admin"
	RoleUser     Role = "user"
	RoleReadOnly Role = "readonly"
)

// Permission names a capability checked on the server for every request. Hiding a
// button in the UI is never a substitute for one of these checks.
type Permission string

const (
	PermClusterRead   Permission = "cluster.read"
	PermClusterWrite  Permission = "cluster.write"
	PermClusterManage Permission = "cluster.manage"
	PermUserManage    Permission = "user.manage"
	PermAuditRead     Permission = "audit.read"
	PermSettingsWrite Permission = "settings.write"
)

var rolePermissions = map[Role]map[Permission]bool{
	RoleAdmin: {
		PermClusterRead: true, PermClusterWrite: true, PermClusterManage: true,
		PermUserManage: true, PermAuditRead: true, PermSettingsWrite: true,
	},
	RoleUser: {
		PermClusterRead: true, PermClusterWrite: true,
	},
	RoleReadOnly: {
		PermClusterRead: true,
	},
}

func ParseRole(s string) (Role, error) {
	switch Role(s) {
	case RoleAdmin, RoleUser, RoleReadOnly:
		return Role(s), nil
	default:
		return "", fmt.Errorf("unknown role %q", s)
	}
}

// Can reports whether the role grants the permission.
func (r Role) Can(p Permission) bool {
	return rolePermissions[r][p]
}

// Permissions lists everything the role grants, for the /me endpoint. The UI uses this
// to decide what to show — never to decide what is allowed.
func (r Role) Permissions() []Permission {
	granted := rolePermissions[r]
	out := make([]Permission, 0, len(granted))
	for _, p := range []Permission{
		PermClusterRead, PermClusterWrite, PermClusterManage,
		PermUserManage, PermAuditRead, PermSettingsWrite,
	} {
		if granted[p] {
			out = append(out, p)
		}
	}
	return out
}

// RequiresMFA reports whether the role must complete a second factor. Admins hold
// cluster-wide destructive power, so a password alone is not enough.
func (r Role) RequiresMFA(enforceForAdmin bool) bool {
	return enforceForAdmin && r == RoleAdmin
}
