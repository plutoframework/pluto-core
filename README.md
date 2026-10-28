# Pluto Core

Focused shared Go runtime for Pluto cursor pagination and idempotent business
writes. Go 1.25.5+, MIT. The root module uses only the standard library. The
optional `github.com/plutoframework/pluto-core/stores/postgres` module uses the
reviewed pgx/v5 v5.11.0 driver and generated typed sqlc queries.

## Code tour and API

- `pagination`: `Binding(query, verifiedScope...)`, `Parse(query, binding)`,
  `Request.Validate/FetchLimit`, `Finish(request, rows, key)`, `Page[T]` and
  `Metadata`. Default 25, maximum 100; oversized/malformed/duplicate parameters
  fail rather than silently clamp. Repositories must validate requests and apply
  `LIMIT limit+1` before returning rows. No count query or offset API.
- `idempotency`: `Scope`, `NewClaim`, `Store.Begin/Complete`, `Response` and
  explicit conflict/in-progress/ownership errors. A random owner token fences
  completion; key namespaces hash verified subject, optional tenant, stable
  operation ID and the client key. Keys are 1–128 ASCII letters/digits/`-_.:`.
- `idempotency/memory`: bounded local demo store. Explicit construction only;
  process restart loses claims. Never substitute it silently for durable storage.
- `httpidem`: bounded JSON business-response adapter. Invoke `Serve` only AFTER
  authentication, current RBAC/feature gates and trusted tenant resolution.
  Session lifecycle, credential/token, streaming and sensitive responses must be
  exempt. The application chooses eligible business routes. Nil storage fails
  constructor validation. `Status` supports thin app error-envelope adapters.
- `stores/postgres`: independent module, reference `schema.sql`, `New(pool)` for
  claims, `New(tx).Complete` for atomic completion and explicit bounded
  `PruneCompleted`. No startup schema work. See [transaction example](docs/transactions.md).

## Pagination contract

Use an immutable ascending deterministic order and unique ID tie-breaker. Pass
that last sort value and ID to `Finish`; query subsequent rows with the matching
strict tuple `>` predicate. ID-only order uses empty `Key.Sort`. Cursors are
bounded (2 KiB), opaque base64url, versioned JSON with a SHA-256 binding to the
operation, verified visibility scope and canonical filters. They are **unsigned**:
a client can change a boundary. They never grant access. Always reapply tenant,
ownership and visibility predicates in every repository query. Bind changes in
visibility policy too. There is no secret/key provisioning requirement and no
claim of tamper resistance or snapshot isolation. Inserts before the boundary
will not appear in later pages; mutable sort keys can cause omissions/repeats.

`Finish` consumes at most limit+1 prefiltered rows; it removes only the lookahead
row and returns `data` and `page: {has_more,next_cursor}`. Empty/final pages return
an empty cursor. Limit changes are permitted between pages. Do not fetch all rows
and slice them; the example uses bounded SQL and bounded in-memory selection.

## Idempotency guarantees and limits

Clients supply a key per intended action and reuse it only for retries. Same
scoped key and fingerprint replays bytes/status; different fingerprints return
409; an unfinished request returns 409 and never runs again automatically.
Fingerprints cover method, escaped resource path, exact query, content type and
SHA-256 of exact body bytes, using bounded reads. Identical bodies with different
keys remain distinct actions. No body-only deduplication or generated retry keys.

TTL defaults to 24h (maximum 7d), from initial claim. Completed entries may be
replaced after expiry: duplicate effects become possible outside this guarantee
window. Completion does not renew it. Pending entries are NEVER expired/pruned
or stolen automatically, including after cancellation, crash, panic, oversized
response or uncertain DB completion. Operators must reconcile pending claims
against business records/external systems before explicitly deleting/resolving
records; monitor their age/capacity. Schedule bounded completed cleanup and
provision storage/HTTP rate limits. Memory capacity exhaustion fails closed.
Postgres disk quotas and cleanup scheduling belong to the application operator.

HTTP body/response hard maximum is 1 MiB, with explicit smaller adapter limits.
Only JSON statuses 200–499 excluding 401/403 are replayable. Application
401/403/5xx responses are delivered without caching and leave claims pending.
Only Content-Type is recorded. Cookie/auth/location/hop-by-hop response headers
are rejected; arbitrary headers are never cached. Global security headers run
outside replay and stay current. JSON payload sensitivity cannot be detected:
explicitly exempt endpoints returning credentials, tokens or sensitive secrets.
The helper buffers the entire bounded response; it does not implement streaming.

The generic HTTP helper persists completion AFTER the operation, so a crash
between a committed business mutation and completion blocks retries pending
reconciliation. It does not guarantee exactly-once side effects. Use the same
pgx transaction for mutation, audit and completion when atomicity is required.
External calls need a domain-specific protocol/outbox; arbitrary transactions
are never retried. A replay still requires current route authorization; resource
policies are the application's responsibility and may need a pre-replay check
where access can change independently of membership/route permission.

## Verification

```sh
./scripts/fmt.sh
./scripts/check.sh
./scripts/test.sh -race
./scripts/fuzz.sh
python3 scripts/mutation-smoke.py
./scripts/postgres-check.sh
(cd stores/postgres && go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.29.0 generate)
```

The disposable SQL script tests real concurrent ownership and same-transaction
business mutation/completion rollback and commit. Without
`PLUTO_CORE_TEST_DATABASE_URL`, SQL tests explicitly skip; CI supplies a database.
The mutation smoke is small, seeded guard verification, not a security audit.
This MVP is pre-release and has not undergone independent security review.
