# Shipment PostgreSQL reference consumer (unpublished)

This separate application example demonstrates a concrete mandatory lifecycle
write boundary. It is not a generic framework storage API. It uses the candidate
Core source via an explicit local replacement; the established PostgreSQL
adapter
module and all published dependency pins remain unchanged. Release-pin consumer
validation is still required before publishing this API.

Composition constructs one `shipments.NewLifecycle()` machine and injects that
same instance into the authorized operation and `shipmentpostgres.New(pool,
machine)`. The operation loads a tenant-scoped snapshot, verifies current
access,
and calls `shipments.DispatchLoaded(ctx, machine, writer, snapshot)` . The
consumer
port accepts only `state.Evaluated[shipments.Status, shipments.Event]` . The
writer
rejects zero or foreign-machine tokens before accessing PostgreSQL. There is no
public replacement-record write method or arbitrary write callback.

Apply `Schema` explicitly in application migrations; construction never executes
DDL. The concrete writer owns its transaction and sqlc queries: it atomically
matches tenant ID, shipment ID, source status and expected version, updates
status
and version, then inserts dispatch intent in the same transaction. Missing/stale
snapshots and replay fail with `ErrConflict` . Outbox failure rolls back the
state
update. The outbox row is this small example's audit record; a worker owns
actual
delivery. An uncertain commit error must be reconciled instead of blindly
retried.

Every write changing address verification or other guard facts must advance the
same version. The writer does not establish authorization, refresh guard facts,
or enforce consumer adoption elsewhere. Callers with independent SQL credentials
can bypass an application port; restrict those capabilities and enforce declared
ports through application architecture checks. Existing public snapshot fields
are read facts, not accepted replacement write payloads. Multi-row guard facts
would require a suitable additional transaction/isolation design.

From this module:

```sh
GOWORK=off make fmt
GOWORK=off make vet
GOWORK=off make check
PLUTO_CORE_TEST_DATABASE_URL='<disposable PostgreSQL URL>' make database
```

`make database` requires its URL, fails on skipped or missing required cases,
and
runs the real SQL tests with race detection. Ordinary tests explicitly skip SQL
cases without the URL; a general test pass is not database evidence. From the
Core repository root, `scripts/postgres-check.sh` creates a disposable
PostgreSQL
17.6 container with a dynamic loopback port, runs both established adapter cases
and this reference consumer, then removes the container. It never connects to an
existing database. The schema-isolated tests cover all binding predicates, zero
and foreign-machine decisions, concurrent commits, replay, tenant isolation and
rollback when outbox insertion fails. Run
`PLUTO_CORE_SQL_MUTATION=1 scripts/postgres-check.sh` from the Core root for six
real-SQL mutations covering each CAS predicate, using the pool outside the
transaction, and omitting commit. Build failures, timeouts and missing databases
fail the mutation command rather than count as killed mutations. Each mutant
runs
only its exact relevant test/subtest and must emit its intended business
assertion, with no unrelated failure output. Pristine real-SQL controls run
before
and after the mutations. The evidence classifier runs self-tests rejecting
connection/setup errors, SQL errors, build failures, timeouts, absent cases and
wrong assertions before using the database.
`TestRequiredDatabaseGate` proves the
strict command rejects a missing URL.

Regenerate fixed typed queries with the pinned generator:

```sh
GOWORK=off go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.29.0 generate
```

## Candidate complete operation/data profile

The `plutocandidate` build tag enables a stricter application reference using
unpublished Auth/RBAC identity gates and typed Jobs definitions. It is off by
default. Candidate dependencies use explicit local sibling replacements only in
this reference module. Default Core checks remain independently runnable without
those sibling checkouts; no published module pin claims these APIs exist.

With `pluto-auth` , `pluto-rbac` and `pluto-jobs` candidate checkouts alongside
Core:

```sh
PLUTO_CORE_CANDIDATE_DATA=1 make fmt
PLUTO_CORE_CANDIDATE_DATA=1 make vet
PLUTO_CORE_CANDIDATE_DATA=1 make check
PLUTO_CORE_CANDIDATE_DATA=1 ../../../scripts/test.sh -race
```

From the Core root, execute the complete strict SQL profile and semantic
mutants:

```sh
PLUTO_CORE_CANDIDATE_DATA=1 PLUTO_CORE_SQL_MUTATION=1 scripts/postgres-check.sh
```

The application-owned domain constructors create private `DispatchInput`,
`ListInput` and `ExportInput` values. Dispatch normalizes the shipment ID once;
it preserves the exact validated idempotency key. Invalid zero values, excessive
pages/exports and invalid identifiers fail before reaching data. The shared
`contract` package owns small `DispatchData`, `ListData` and `ExportData` ports;
`operations.Dispatch` orders claim/replay, scoped load, domain evaluation and
commit. Operation code imports contracts and domain rules, never the SQL
adapter.

Composition constructs one domain machine, one concrete identity gate and the
`Data` adapter. The gate must use the trusted Auth Manager, current membership,
permission and resource policy. Inside `Manager.WithIdentity` and `Gate.Within`,
the authored operation invokes:

```go
input, err := shipments.NewDispatchInput(rawShipmentID, exactIdempotencyKey)
// Propagate err. The input constructor is the single normalization boundary.
result, err := operations.Dispatch(ctx, execution, data, lifecycle, input)
```

Every concrete data call invokes that same gate's `Use` before SQL or cache.
Entity methods bind the exact entity resource; collection methods require the
explicit `shipments` collection resource. A collection lease cannot read an
entity even if its policy accepts both resource names. The reference uses one
explicit resource capability covering these methods; applications requiring
separate read/write permissions must inject and declare the corresponding
operation gates rather than interpreting a read grant as write authority.

`StartDispatch` first verifies current row visibility, then creates or replays
the
owned logical claim. It returns an opaque adapter-issued attempt interface;
implementing that interface cannot mint a valid commit attempt. The concrete
adapter rejects foreign attempts, actors, tenants, resources and machine tokens.
`CommitDispatch` atomically commits the SQL CAS, shipment audit, typed Jobs
request
and idempotency completion. Tests inject audit, delivery, completion and
deferred
commit failures: none may publish a partial transaction. Pending/uncertain
claims
remain pending; retries do not steal them or mint another delivery ID. Current
authorization and visibility are reapplied even before replaying a response.

`Load` reads authoritative SQL facts for lifecycle guards and never consults the
cache. `Display` performs a fresh row-visibility query before even a cache hit.
Cache keys
include verified actor, tenant, entity and fixed schema namespace/version. Cache
snapshots remain non-authoritative and TTL-bounded; every relevant fact change
must advance entity version. Post-commit invalidation is best effort and cannot
undo a commit. `List` applies actor/tenant SQL visibility before keyset ordering
and limit. Cursors bind actor, tenant and visibility policy. `Export` reuses
only
that query, refreshes authority each page and errors instead of silently
truncating beyond the explicit maximum. These are live keyset reads, not a
point-in-time transactional export snapshot under concurrent changes.

The Jobs outbox is delivered after commit through a kind-bound typed handler and
a newly established worker operation identity. Stored tenant metadata is never
authority. Jobs preserves finite attempts, stable delivery ID and per-claim
fencing. `DeliverySchema` is an explicit snapshot of the candidate Jobs
migration;
apply it, the Core idempotency schema and the application schema deliberately.
Constructors perform no DDL and no transaction spans external delivery.

The candidate strict runner requires all nine shipment SQL cases plus the two
existing adapter cases. Fourteen SQL mutations require the exact intended
business assertion, with pristine controls before and after; malformed setup,
missing dependencies/databases, skips, build failures and timeouts cannot count
as semantic rejection. Independent compiler, runtime, SQL and generated-consumer
adoption remain separate proof boundaries. The root Core workflow exercises the
default standalone profile; a candidate integration job must opt into the tag
with its verified sibling checkouts. No hosted branch protections are implied.

The corrupted-cache regression deliberately stores a display snapshot claiming
address verification while the authoritative row is unverified at the same
state/version. Display reaches that cache entry as a positive control; the
transition operation still denies dispatch from authoritative SQL facts. This
covers corruption, not just ordinary cache freshness. Replacing authoritative
`Load` with cached display access is an explicit semantic mutation.
