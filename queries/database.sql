-- name: EnsureSchemaMigrationsTable :exec
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- name: IsMigrationApplied :one
SELECT EXISTS(
    SELECT 1
    FROM schema_migrations
    WHERE version = ?
);

-- name: RecordMigration :exec
INSERT INTO schema_migrations (version)
VALUES (?);

-- name: CountAppliedMigrations :one
SELECT COUNT(*)
FROM schema_migrations;
