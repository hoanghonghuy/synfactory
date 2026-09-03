package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"sort"

	"github.com/hoanghonghuy/synfactory/migrations"
)

const ensureMigrationsTableSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`

// migrationLockKey is stable for every SynFactory process sharing one database.
// A session-level advisory lock serializes startup migrations across API,
// scheduler, and worker processes without coupling service startup order.
const migrationLockKey int64 = 0x53594E46414354

func (s *Store) ApplyMigrations(ctx context.Context) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, migrationLockKey); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, migrationLockKey)
	}()

	if _, err := conn.ExecContext(ctx, ensureMigrationsTableSQL); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}

	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(names)

	for _, name := range names {
		applied, err := migrationApplied(ctx, conn, name)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		contents, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, string(contents)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}

	return nil
}

type migrationQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func migrationApplied(ctx context.Context, queryer migrationQueryer, name string) (bool, error) {
	var applied bool
	err := queryer.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, name,
	).Scan(&applied)
	if err != nil {
		return false, fmt.Errorf("check migration %s: %w", name, err)
	}
	return applied, nil
}
