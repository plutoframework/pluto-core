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

## Optional OpenTelemetry

`github.com/plutoframework/pluto-core/telemetry` is an independently versioned
adapter module; root consumers retain standard-library-only dependencies.
See [telemetry setup and privacy contract](telemetry/README.md) for explicit
providers, OTLP export, Chi HTTP tracing/metrics, outbound HTTP propagation,
PostgreSQL query tracing, cache/outbox/job decorators and slog correlation.
Applications own startup configuration and bounded shutdown. Diagnostics never
replace durable business or security audit records.

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

## Optional data caching

Core v0.2.0 adds stdlib-only `cache` and `cache/memory`. The separately versioned
`stores/valkey` module adapts standalone Valkey/Redis using valkey-go v1.0.79.
The root module gains no third-party runtime dependency. The adapter's runtime
imports are audited independently of test-only module requirements.

Store.Get returns bytes, found and error separately: an empty value is a hit,
while an outage is an error. Set requires a positive TTL from 1ms to the configured
maximum (default 24h, configurable ceiling 7d). Default key/value bounds are 512
bytes/1MiB. Memory is LRU-bounded by entries and combined key/value bytes; allocator
and map/list overhead are not counted in that byte budget. Inputs/outputs are copied.

Use `cache.Key(cache.Scope{App: ..., Environment: ..., Namespace: ..., Version: ...,
TenantID: verifiedTenant, SubjectID: verifiedSubject}, resourceID)`. Length-framed
hashing prevents separator collisions and keeps raw identifiers out of keys.
Empty tenant/subject explicitly denotes shared data, not authorization. Version
belongs to the cached data schema. Never derive authority from a client header.

`cache.GetJSON[T]`, `SetJSON[T]` and `NewLoader[T]` provide typed JSON ergonomics.
Loader.Remember coalesces concurrent calls within one loader/process, bounds active
keys/waiters and gives the leader a cooperative load deadline. Followers can cancel
independently; canceling the leader fails that flight. Returned JSON values have
independent ownership. Successful values only are cached, never loader errors.
Default Bypass loads from the database on backend/corruption errors; Strict returns
those errors. Invalid options, unencodable or oversized loaded values are contract
errors, not ordinary cache outages. Hooks receive fixed sentinels, never payloads.

Data caches may be stale. Delete after successful commits; deletion can race an
in-flight loader and does not prove read-after-write consistency. Versioned keys,
stronger application coordination or bypass are needed where that matters. Keep
identity, membership, permission checks, session revocation, locks, rate limits and
durable idempotency outside this interface. Auth responses and secrets are not
cache entries. No automatic HTTP response caching is provided.

Valkey New validates configuration without dialing; the first operation connects,
with bounded/coalesced initial retries after outages. Open additionally checks
connectivity. Close belongs to the caller. Operations have explicit timeouts; only
GET/SET PX/DEL are used. Client-side caching and topology discovery are disabled.
Plaintext requires loopback or explicit AllowInsecure; rediss uses verified TLS.
URL query/fragment options are rejected. No raw server errors/URLs are returned.
The adapter rejects oversized results after the client receives them; the shared
byte limit does not bound the third-party client's wire parsing allocations.

Run `./scripts/cache-check.sh` for real authenticated Valkey 9.1.3 and Redis 8.10.3
contract/race tests in disposable containers. These are the tested standalone
versions, not a promise of all Redis versions, clusters or Sentinel support.
Caching is optional; external adapters are not a new mandatory service for apps.

## Business lifecycles

Import `github.com/plutoframework/pluto-core/state` for typed, explicit states,
events and pure business guards. `New` validates the definition once; `Next`
decides a transition without mutating an entity. Unknown transitions fail
closed. The caller retains authorization and atomic persistence ownership.

See [the state package](state/README.md) and the compiling
[purchase lifecycle](examples/purchases/lifecycle.go). The Pluto CLI command
`pluto add state GROUP` scaffolds an authored lifecycle and its tests.
