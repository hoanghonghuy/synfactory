package postgres

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/authz"
)

func TestAuthSessionPersistsScopedGrantsRevocationAndAudit(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	userID := "auth-user-integration"
	_, _ = store.db.ExecContext(ctx, `DELETE FROM auth_audit WHERE user_id = $1`, userID)
	_, _ = store.db.ExecContext(ctx, `DELETE FROM auth_users WHERE id = $1`, userID)
	t.Cleanup(func() {
		_, _ = store.db.ExecContext(context.Background(), `DELETE FROM auth_audit WHERE user_id = $1`, userID)
		_, _ = store.db.ExecContext(context.Background(), `DELETE FROM auth_users WHERE id = $1`, userID)
	})

	if err := store.UpsertAuthUser(ctx, userID, "github", "github-subject-1", "Alice"); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceAuthGrants(ctx, userID,
		[]authz.RoleGrant{{Role: authz.RoleOperator, RepositoryID: "repo-a"}},
		[]authz.PermissionGrant{{Permission: authz.PermissionTerminalAccess, RepositoryID: "repo-a"}},
	); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 9, 4, 14, 0, 0, 0, time.UTC)
	tokenHash := sha256.Sum256([]byte("opaque-integration-token"))
	if err := store.CreateAuthSession(ctx, "auth-session-integration", userID, tokenHash, now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}

	session, err := store.ResolveSession(ctx, tokenHash)
	if err != nil {
		t.Fatal(err)
	}
	if session.Principal.Subject != userID || session.Principal.DisplayName != "Alice" {
		t.Fatalf("unexpected principal: %+v", session.Principal)
	}
	if !session.Principal.Allowed(authz.PermissionRepositoryMutate, "repo-a") {
		t.Fatal("repository-scoped operator grant was not loaded")
	}
	if session.Principal.Allowed(authz.PermissionRepositoryMutate, "repo-b") {
		t.Fatal("repository-scoped operator grant escaped its scope")
	}
	if !session.Principal.Allowed(authz.PermissionTerminalAccess, "repo-a") {
		t.Fatal("explicit terminal grant was not loaded")
	}

	if err := store.RecordAuthorization(ctx, session.ID, session.Principal, authz.PermissionTerminalAccess, "repo-a", true, "allowed", now); err != nil {
		t.Fatal(err)
	}
	var auditCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_audit WHERE user_id = $1 AND session_id = $2 AND allowed = TRUE`, userID, session.ID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("audit count = %d, want 1", auditCount)
	}

	if err := store.RevokeAuthSession(ctx, session.ID, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	revoked, err := store.ResolveSession(ctx, tokenHash)
	if err != nil {
		t.Fatal(err)
	}
	if revoked.RevokedAt == nil {
		t.Fatal("revocation was not persisted")
	}
}
