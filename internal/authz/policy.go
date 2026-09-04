package authz

import "strings"

type Role string

type Permission string

const (
	RoleAdministrator Role = "administrator"
	RoleOperator      Role = "operator"
	RoleReviewer      Role = "reviewer"
	RoleObserver      Role = "observer"
)

const (
	PermissionRead           Permission = "read"
	PermissionRepositoryMutate Permission = "repository_mutate"
	PermissionTerminalAccess Permission = "terminal_access"
	PermissionReleasePromote Permission = "release_promote"
	PermissionSecurityPolicy Permission = "security_policy"
)

type RoleGrant struct {
	Role         Role   `json:"role"`
	RepositoryID string `json:"repository_id,omitempty"`
}

type PermissionGrant struct {
	Permission   Permission `json:"permission"`
	RepositoryID string     `json:"repository_id,omitempty"`
}

type Principal struct {
	Subject     string            `json:"subject"`
	DisplayName string            `json:"display_name,omitempty"`
	Roles       []RoleGrant       `json:"roles,omitempty"`
	Permissions []PermissionGrant `json:"permissions,omitempty"`
}

func (p Principal) Allowed(permission Permission, repositoryID string) bool {
	repositoryID = strings.TrimSpace(repositoryID)
	for _, grant := range p.Roles {
		if !grantApplies(grant.RepositoryID, repositoryID) {
			continue
		}
		switch grant.Role {
		case RoleAdministrator:
			return true
		case RoleOperator:
			if permission == PermissionRead || permission == PermissionRepositoryMutate {
				return true
			}
		case RoleReviewer, RoleObserver:
			if permission == PermissionRead {
				return true
			}
		}
	}
	for _, grant := range p.Permissions {
		if grant.Permission == permission && grantApplies(grant.RepositoryID, repositoryID) {
			return true
		}
	}
	return false
}

func grantApplies(grantRepositoryID, requestedRepositoryID string) bool {
	grantRepositoryID = strings.TrimSpace(grantRepositoryID)
	if grantRepositoryID == "" {
		return true
	}
	return requestedRepositoryID != "" && grantRepositoryID == requestedRepositoryID
}
