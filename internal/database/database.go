package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Tarat0r/namepoll/internal/database/migrationsqlc"
	sqlc "github.com/Tarat0r/namepoll/internal/database/sqlc"
	_ "modernc.org/sqlite"
)

func Open(path string) (*sql.DB, error) {
	dsn := path + "?_foreign_keys=on&_journal_mode=wal&_busy_timeout=5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// SQLite pragmas are connection-local. A single shared connection keeps
	// foreign-key and busy-timeout behavior consistent for this small service.
	db.SetMaxOpenConns(1)
	if err := migrate(context.Background(), db); err != nil {
		db.Close()
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return db, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	queries := sqlc.New(db)
	if err := queries.EnsureSchemaMigrationsTable(ctx); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}

	for _, migration := range migrations {
		applied, err := queries.IsMigrationApplied(ctx, migration.version)
		if err != nil {
			return fmt.Errorf("check migration %d: %w", migration.version, err)
		}
		if applied {
			continue
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", migration.version, err)
		}
		if err := migration.apply(ctx, migrationsqlc.New(tx)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", migration.version, err)
		}
		if err := queries.WithTx(tx).RecordMigration(ctx, migration.version); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %d: %w", migration.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", migration.version, err)
		}
	}

	return nil
}

type migration struct {
	version int64
	apply   func(context.Context, *migrationsqlc.Queries) error
}

var migrations = []migration{
	{version: 1, apply: applyMigration001},
	{version: 2, apply: applyMigration002},
	{version: 3, apply: applyMigration003},
}

func applyMigration001(ctx context.Context, queries *migrationsqlc.Queries) error {
	if err := queries.Migration001CreateSubmissions(ctx); err != nil {
		return err
	}
	return queries.Migration001CreateSuggestions(ctx)
}

func applyMigration002(ctx context.Context, queries *migrationsqlc.Queries) error {
	steps := []func(context.Context) error{
		queries.Migration002CreateSubmissionsReplacement,
		queries.Migration002CopySubmissions,
		queries.Migration002AddSuggestionField,
		queries.Migration002CreateSuggestionsReplacement,
		queries.Migration002CopySuggestions,
		queries.Migration002DropOldSuggestions,
		queries.Migration002DropOldSubmissions,
		queries.Migration002RenameSubmissions,
		queries.Migration002RenameSuggestions,
	}
	for _, step := range steps {
		if err := step(ctx); err != nil {
			return err
		}
	}
	return nil
}

func applyMigration003(ctx context.Context, queries *migrationsqlc.Queries) error {
	return queries.Migration003CreateRateLimits(ctx)
}
