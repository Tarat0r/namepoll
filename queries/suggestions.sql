-- name: CreateSuggestion :one
INSERT INTO suggestions (
    submission_id,
    field_name,
    name
)
VALUES (?, ?, ?)
RETURNING *;

-- name: CreateSubmission :one
INSERT INTO submissions (
    token_hash,
    author_name
)
VALUES (?, ?)
RETURNING *;

-- name: ListSuggestions :many
SELECT *
FROM suggestions
ORDER BY created_at DESC;

-- name: GetSubmissionByTokenHash :one
SELECT id, author_name, token_hash, created_at
FROM submissions
WHERE token_hash = ?;

-- name: ListSuggestionStats :many
SELECT field_name, name, COUNT(*) AS suggestion_count
FROM suggestions
GROUP BY field_name, name
ORDER BY field_name, suggestion_count DESC, name;

-- name: ListSuggestionVoters :many
SELECT suggestions.field_name, suggestions.name, submissions.author_name
FROM suggestions
JOIN submissions ON submissions.id = suggestions.submission_id
ORDER BY suggestions.field_name, suggestions.name, submissions.created_at, submissions.id;

-- name: CountSubmissions :one
SELECT COUNT(*) FROM submissions;

-- name: CountSuggestions :one
SELECT COUNT(*) FROM suggestions;
