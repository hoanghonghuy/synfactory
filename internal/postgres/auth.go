package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/authz"
)

func (s *Store) FindAuthUserByExternalIdentity(ctx context.Context, provider, providerSubject string) (string, bool, error) {
	provider = strings.TrimSpace(provider)
	providerSubject = strings.TrimSpace(providerSubject)
	if provider == "" || providerSubject == "" {
		return "", false, fmt.Errorf("provider and provider subject are required")
	}
	var id string
	err := s.db.QueryRowContext(ctx, `
SELECT id FROM auth_users WHERE provider = $1 AND provider_subject = $2`, provider, providerSubject).Scan(&id)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("find auth user by external identity: %w", err)
	}
	return id, true, nil
}

func (s *Store) UpsertAuthUser(ctx context.Context, id, provider, providerSubject, displayName string) error {
	id = strings.TrimSpace(id)
	provider = strings.TrimSpace(provider)
	providerSubject = strings.TrimSpace(providerSubject)
	if id == "" || provider == "" || providerSubject == "" {
		return fmt.Errorf("auth user id, provider and provider subject are required")
	}
	var canonicalID string
	err := s.db.QueryRowContext(ctx, `
INSERT INTO auth_users (id, provider, provider_subject, display_name)
VALUES ($1, $2, $3, $4)
ON CONFLICT (provider, provider_subject) DO UPDATE
SET display_name = EXCLUDED.display_name,
    updated_at = NOW()
RETURNING id`, id, provider, providerSubject, strings.TrimSpace(displayName)).Scan(&canonicalID)
	if err != nil {
		return fmt.Errorf("upsert auth user: %w", err)
	}
	if canonicalID != id {
		return fmt.Errorf("external identity %s/%s is already bound to auth user %s", provider, providerSubject, canonicalID)
	}
	return nil
}

func (s *Store) ReplaceAuthGrants(ctx context.Context, userID string, roles []authz.RoleGrant, permissions []authz.PermissionGrant) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return fmt.Errorf("auth user id is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin auth grant replacement: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_role_grants WHERE user_id = $1`, userID); err != nil {
		return fmt.Errorf("clear auth role grants: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_permission_grants WHERE user_id = $1`, userID); err != nil {
		return fmt.Errorf("clear auth permission grants: %w", err)
	}
	for _, grant := range roles {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_role_grants (user_id, role, repository_id)
VALUES ($1, $2, $3)`, userID, grant.Role, strings.TrimSpace(grant.RepositoryID)); err != nil {
			return fmt.Errorf("insert auth role grant: %w", err)
		}
	}
	for _, grant := range permissions {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_permission_grants (user_id, permission, repository_id)
VALUES ($1, $2, $3)`, userID, grant.Permission, strings.TrimSpace(grant.RepositoryID)); err != nil {
			return fmt.Errorf("insert auth permission grant: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit auth grant replacement: %w", err)
	}
	return nil
}

func (s *Store) CreateAuthSession(ctx context.Context, id, userID string, tokenHash [sha256.Size]byte, expiresAt, now time.Time) error {
	id = strings.TrimSpace(id)
	userID = strings.TrimSpace(userID)
	if id == "" || userID == "" {
		return fmt.Errorf("auth session id and user id are required")
	}
	if !expiresAt.After(now) {
		return fmt.Errorf("auth session expiry must be in the future")
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO auth_sessions (id, user_id, token_hash, expires_at, created_at, last_seen_at)
VALUES ($1, $2, $3, $4, $5, $5)`, id, userID, tokenHash[:], expiresAt.UTC(), now.UTC())
	if err != nil {
		return fmt.Errorf("create auth session: %w", err)
	}
	return nil
}

func (s *Store) RevokeAuthSession(ctx context.Context, id string, revokedAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE auth_sessions
SET revoked_at = COALESCE(revoked_at, $2)
WHERE id = $1`, strings.TrimSpace(id), revokedAt.UTC())
	if err != nil {
		return fmt.Errorf("revoke auth session: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("revoke auth session rows: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ResolveSession(ctx context.Context, tokenHash [sha256.Size]byte) (authz.SessionRecord, error) {
	var record authz.SessionRecord
	var revokedAt sql.NullTime
	var disabled bool
	err := s.db.QueryRowContext(ctx, `
SELECT s.id, s.expires_at, s.revoked_at, u.id, u.display_name, u.disabled
FROM auth_sessions s
JOIN auth_users u ON u.id = s.user_id
WHERE s.token_hash = $1`, tokenHash[:]).Scan(
		&record.ID,
		&record.ExpiresAt,
		&revokedAt,
		&record.Principal.Subject,
		&record.Principal.DisplayName,
		&disabled,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return authz.SessionRecord{}, ErrNotFound
		}
		return authz.SessionRecord{}, fmt.Errorf("resolve auth session: %w", err)
	}
	if disabled {
		return authz.SessionRecord{}, ErrNotFound
	}
	if revokedAt.Valid {
		revoked := revokedAt.Time.UTC()
		record.RevokedAt = &revoked
	}

	roleRows, err := s.db.QueryContext(ctx, `SELECT role, repository_id FROM auth_role_grants WHERE user_id = $1 ORDER BY role, repository_id`, record.Principal.Subject)
	if err != nil {
		return authz.SessionRecord{}, fmt.Errorf("load auth role grants: %w", err)
	}
	for roleRows.Next() {
		var grant authz.RoleGrant
		if err := roleRows.Scan(&grant.Role, &grant.RepositoryID); err != nil {
			_ = roleRows.Close()
			return authz.SessionRecord{}, fmt.Errorf("scan auth role grant: %w", err)
		}
		record.Principal.Roles = append(record.Principal.Roles, grant)
	}
	if err := roleRows.Close(); err != nil {
		return authz.SessionRecord{}, fmt.Errorf("close auth role grants: %w", err)
	}
	if err := roleRows.Err(); err != nil {
		return authz.SessionRecord{}, fmt.Errorf("iterate auth role grants: %w", err)
	}

	permissionRows, err := s.db.QueryContext(ctx, `SELECT permission, repository_id FROM auth_permission_grants WHERE user_id = $1 ORDER BY permission, repository_id`, record.Principal.Subject)
	if err != nil {
		return authz.SessionRecord{}, fmt.Errorf("load auth permission grants: %w", err)
	}
	for permissionRows.Next() {
		var grant authz.PermissionGrant
		if err := permissionRows.Scan(&grant.Permission, &grant.RepositoryID); err != nil {
			_ = permissionRows.Close()
			return authz.SessionRecord{}, fmt.Errorf("scan auth permission grant: %w", err)
		}
		record.Principal.Permissions = append(record.Principal.Permissions, grant)
	}
	if err := permissionRows.Close(); err != nil {
		return authz.SessionRecord{}, fmt.Errorf("close auth permission grants: %w", err)
	}
	if err := permissionRows.Err(); err != nil {
		return authz.SessionRecord{}, fmt.Errorf("iterate auth permission grants: %w", err)
	}
	return record, nil
}

func (s *Store) RecordAuthorization(ctx context.Context, sessionID string, principal authz.Principal, permission authz.Permission, repositoryID string, allowed bool, reason string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO auth_audit (user_id, session_id, action, permission, repository_id, allowed, reason, created_at)
VALUES ($1, $2, 'authorize', $3, $4, $5, $6, $7)`,
		strings.TrimSpace(principal.Subject), strings.TrimSpace(sessionID), permission, strings.TrimSpace(repositoryID), allowed, strings.TrimSpace(reason), at.UTC())
	if err != nil {
		return fmt.Errorf("record authorization audit: %w", err)
	}
	return nil
}
