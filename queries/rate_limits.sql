-- name: PruneRateLimits :exec
DELETE FROM rate_limits
WHERE last_attempt <= ?;

-- name: GetRateLimit :one
SELECT key_hash, last_attempt
FROM rate_limits
WHERE key_hash = ?;

-- name: UpsertRateLimit :exec
INSERT INTO rate_limits (key_hash, last_attempt)
VALUES (?, ?)
ON CONFLICT(key_hash) DO UPDATE
SET last_attempt = excluded.last_attempt;
