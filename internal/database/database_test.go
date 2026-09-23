package database

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tarat0r/namepoll/internal/database/migrationsqlc"
	sqlc "github.com/Tarat0r/namepoll/internal/database/sqlc"
)

func TestOpenMigratesLegacyDatabaseAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	legacyQueries := migrationsqlc.New(legacy)
	if err := legacyQueries.Migration001CreateSubmissions(context.Background()); err != nil {
		t.Fatalf("create legacy submissions schema: %v", err)
	}
	if err := legacyQueries.Migration001CreateSuggestions(context.Background()); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	db, err := Open(path)
	if err != nil {
		t.Fatalf("migrate legacy database: %v", err)
	}

	queries := sqlc.New(db)
	migrations, err := queries.CountAppliedMigrations(context.Background())
	if err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if migrations != 3 {
		t.Fatalf("migration count = %d, want 3", migrations)
	}

	submission, err := queries.CreateSubmission(context.Background(), sqlc.CreateSubmissionParams{
		TokenHash:  sql.NullString{String: "token-hash", Valid: true},
		AuthorName: sql.NullString{String: "Ада", Valid: true},
	})
	if err != nil {
		t.Fatalf("create submission after migration: %v", err)
	}
	if _, err := queries.CreateSuggestion(context.Background(), sqlc.CreateSuggestionParams{
		SubmissionID: submission.ID,
		FieldName:    "suggestions",
		Name:         "Нова",
	}); err != nil {
		t.Fatalf("create suggestion after migration: %v", err)
	}
	suggestions, err := queries.ListSuggestions(context.Background())
	if err != nil {
		t.Fatalf("read migrated suggestion: %v", err)
	}
	if len(suggestions) != 1 || suggestions[0].FieldName != "suggestions" || suggestions[0].Name != "Нова" {
		t.Fatalf("migrated suggestions = %#v", suggestions)
	}

	if err := queries.UpsertRateLimit(context.Background(), sqlc.UpsertRateLimitParams{
		KeyHash:     "hash",
		LastAttempt: time.Now(),
	}); err != nil {
		t.Fatalf("rate_limits table unavailable: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close migrated database: %v", err)
	}

	db, err = Open(path)
	if err != nil {
		t.Fatalf("reopen migrated database: %v", err)
	}
	db.Close()
}

func TestOpenCreatesFreshDatabase(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatalf("open fresh database: %v", err)
	}
	defer db.Close()

	queries := sqlc.New(db)
	if _, err := queries.CountSubmissions(context.Background()); err != nil {
		t.Fatalf("submissions table unavailable: %v", err)
	}
	if _, err := queries.CountSuggestions(context.Background()); err != nil {
		t.Fatalf("suggestions table unavailable: %v", err)
	}
	if _, err := queries.CountAppliedMigrations(context.Background()); err != nil {
		t.Fatalf("schema_migrations table unavailable: %v", err)
	}
	if _, err := queries.GetRateLimit(context.Background(), "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("rate_limits table check error = %v, want sql.ErrNoRows", err)
	}
}
