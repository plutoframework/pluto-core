# Working on Pluto Core

Keep the runtime focused on pagination and idempotency. Root packages are
stdlib-only. PostgreSQL/pgx stays in the separate stores/postgres module.
Use explicit constructor injection, typed sqlc queries, application-owned
migrations, no startup DDL, globals or init registration. Never trust a cursor
or client identity/tenant header as authorization. Never steal unfinished
idempotency claims or imply exactly-once external effects.

Keep every file below 1,000 lines and use the configurable 80-column expanded
gci/gofumpt/golines + wsl_v5 format. Tooling pins golangci-lint 2.15.0.
Run scripts/fmt.sh, scripts/check.sh, scripts/test.sh -race, scripts/fuzz.sh,
scripts/mutation-smoke.py and scripts/postgres-check.sh for security changes.
SQL tests require a disposable database; a skip is not a pass.
