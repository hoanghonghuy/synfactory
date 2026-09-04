package authz

import "testing"

func TestRepositoryScopedRoleCannotEscapeScope(t *testing.T) {
	principal := Principal{Subject: "alice", Roles: []RoleGrant{{Role: RoleOperator, RepositoryID: "repo-a"}}}
	if !principal.Allowed(PermissionRepositoryMutate, "repo-a") {
		t.Fatal("operator should mutate the granted repository")
	}
	if principal.Allowed(PermissionRepositoryMutate, "repo-b") {
		t.Fatal("repository-scoped role escaped its grant")
	}
	if principal.Allowed(PermissionRepositoryMutate, "") {
		t.Fatal("repository-scoped role must not become a global grant")
	}
}

func TestSensitivePermissionsRemainIndependentFromOperatorRole(t *testing.T) {
	principal := Principal{Subject: "operator", Roles: []RoleGrant{{Role: RoleOperator}}}
	if !principal.Allowed(PermissionRead, "repo-a") || !principal.Allowed(PermissionRepositoryMutate, "repo-a") {
		t.Fatal("operator base permissions are incomplete")
	}
	for _, permission := range []Permission{PermissionTerminalAccess, PermissionReleasePromote, PermissionSecurityPolicy} {
		if principal.Allowed(permission, "repo-a") {
			t.Fatalf("operator unexpectedly inherited sensitive permission %q", permission)
		}
	}

	principal.Permissions = []PermissionGrant{{Permission: PermissionTerminalAccess, RepositoryID: "repo-a"}}
	if !principal.Allowed(PermissionTerminalAccess, "repo-a") {
		t.Fatal("explicit terminal permission was not honored")
	}
	if principal.Allowed(PermissionTerminalAccess, "repo-b") {
		t.Fatal("repository-scoped terminal permission escaped its grant")
	}
	if principal.Allowed(PermissionReleasePromote, "repo-a") {
		t.Fatal("terminal permission must not imply release promotion")
	}
}

func TestObserverAndReviewerAreReadOnlyByDefault(t *testing.T) {
	for _, role := range []Role{RoleObserver, RoleReviewer} {
		principal := Principal{Subject: string(role), Roles: []RoleGrant{{Role: role}}}
		if !principal.Allowed(PermissionRead, "repo-a") {
			t.Fatalf("%s should have read access", role)
		}
		if principal.Allowed(PermissionRepositoryMutate, "repo-a") {
			t.Fatalf("%s unexpectedly has repository mutation access", role)
		}
	}
}

func TestAdministratorRetainsExplicitBreakGlassAuthority(t *testing.T) {
	principal := Principal{Subject: "admin", Roles: []RoleGrant{{Role: RoleAdministrator}}}
	for _, permission := range []Permission{
		PermissionRead,
		PermissionRepositoryMutate,
		PermissionTerminalAccess,
		PermissionReleasePromote,
		PermissionSecurityPolicy,
	} {
		if !principal.Allowed(permission, "repo-a") {
			t.Fatalf("administrator missing permission %q", permission)
		}
	}
}
