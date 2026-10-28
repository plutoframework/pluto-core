-- name: BeginClaim :one
INSERT INTO pluto_idempotency (id, fingerprint, owner, expires_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (id) DO UPDATE SET
    fingerprint = EXCLUDED.fingerprint,
    owner = EXCLUDED.owner,
    expires_at = EXCLUDED.expires_at,
    completed = false, status = 0, content_type = '', body = ''::bytea
WHERE pluto_idempotency.completed AND pluto_idempotency.expires_at <= now()
RETURNING owner;

-- name: ReadClaim :one
SELECT * FROM pluto_idempotency WHERE id = $1;

-- name: CompleteClaim :execrows
UPDATE pluto_idempotency
SET completed = true, status = $4, content_type = $5, body = $6
WHERE id = $1 AND owner = $2 AND fingerprint = $3 AND NOT completed;

-- name: PruneCompleted :execrows
DELETE FROM pluto_idempotency WHERE id IN (
    SELECT id FROM pluto_idempotency
    WHERE completed AND expires_at <= now()
    ORDER BY expires_at LIMIT 1000
);
