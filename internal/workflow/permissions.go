package workflow

import (
	"github.com/hoanghonghuy/synfactory/internal/domain"
	factoryruntime "github.com/hoanghonghuy/synfactory/internal/runtime"
)

func PermissionsForRole(role domain.Role) []factoryruntime.Permission {
	switch role {
	case domain.RolePM:
		return []factoryruntime.Permission{factoryruntime.PermissionReadRepo}
	case domain.RoleTeamLead:
		return []factoryruntime.Permission{factoryruntime.PermissionReadRepo, factoryruntime.PermissionRunCommand, factoryruntime.PermissionReviewPR}
	case domain.RoleDev:
		return []factoryruntime.Permission{factoryruntime.PermissionReadRepo, factoryruntime.PermissionWriteRepo, factoryruntime.PermissionRunCommand}
	case domain.RoleReviewer, domain.RoleQA:
		return []factoryruntime.Permission{factoryruntime.PermissionReadRepo, factoryruntime.PermissionRunCommand, factoryruntime.PermissionReviewPR}
	case domain.RoleCIGuardian:
		return []factoryruntime.Permission{factoryruntime.PermissionReadRepo, factoryruntime.PermissionWriteRepo, factoryruntime.PermissionRunCommand}
	case domain.RoleRelease:
		return []factoryruntime.Permission{factoryruntime.PermissionReadRepo}
	default:
		return nil
	}
}
