-- Reference migration. Apply explicitly in the application's migration history.
CREATE TABLE pluto_idempotency (
    id text PRIMARY KEY CHECK (length(id) = 64),
    fingerprint text NOT NULL CHECK (length(fingerprint) = 64),
    owner text NOT NULL CHECK (length(owner) = 64),
    expires_at timestamptz NOT NULL,
    completed boolean NOT NULL DEFAULT false,
    status integer NOT NULL DEFAULT 0,
    content_type text NOT NULL DEFAULT '',
    body bytea NOT NULL DEFAULT ''::bytea,
    CHECK (octet_length(body) <= 1048576),
    CHECK (NOT completed OR
        (status >= 200 AND status < 500 AND status NOT IN (401,403)
         AND content_type = 'application/json'))
);
CREATE INDEX pluto_idempotency_expiry_idx ON pluto_idempotency (expires_at)
    WHERE completed;
