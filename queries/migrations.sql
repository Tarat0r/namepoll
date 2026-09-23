-- name: Migration001CreateSubmissions :exec
CREATE TABLE IF NOT EXISTS submissions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    author_name TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- name: Migration001CreateSuggestions :exec
CREATE TABLE IF NOT EXISTS suggestions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    submission_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (submission_id, name),
    FOREIGN KEY (submission_id) REFERENCES submissions(id) ON DELETE CASCADE
);

-- name: Migration002CreateSubmissionsReplacement :exec
CREATE TABLE submissions_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    author_name TEXT,
    token_hash TEXT UNIQUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- name: Migration002CopySubmissions :exec
INSERT INTO submissions_new (id, author_name, created_at)
SELECT id, author_name, created_at
FROM submissions;

-- name: Migration002AddSuggestionField :exec
ALTER TABLE suggestions
    ADD COLUMN field_name TEXT NOT NULL DEFAULT 'suggestions';

-- name: Migration002CreateSuggestionsReplacement :exec
CREATE TABLE suggestions_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    submission_id INTEGER NOT NULL,
    field_name TEXT NOT NULL,
    name TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (submission_id, field_name, name),
    FOREIGN KEY (submission_id) REFERENCES submissions_new(id) ON DELETE CASCADE
);

-- name: Migration002CopySuggestions :exec
INSERT INTO suggestions_new (id, submission_id, field_name, name, created_at)
SELECT id, submission_id, field_name, name, created_at
FROM suggestions;

-- name: Migration002DropOldSuggestions :exec
DROP TABLE suggestions;

-- name: Migration002DropOldSubmissions :exec
DROP TABLE submissions;

-- name: Migration002RenameSubmissions :exec
ALTER TABLE submissions_new RENAME TO submissions;

-- name: Migration002RenameSuggestions :exec
ALTER TABLE suggestions_new RENAME TO suggestions;

-- name: Migration003CreateRateLimits :exec
CREATE TABLE rate_limits (
    key_hash TEXT PRIMARY KEY,
    last_attempt DATETIME NOT NULL
);
