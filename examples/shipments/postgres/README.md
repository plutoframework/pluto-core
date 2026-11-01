# Shipment PostgreSQL reference consumer (unpublished)

This separate application example demonstrates a concrete mandatory lifecycle
write boundary. It is not a generic framework storage API. It uses the candidate
Core source via an explicit local replacement; the established PostgreSQL adapter
module and all published dependency pins remain unchanged. Release-pin consumer
validation is still required before publishing this API.

Composition constructs one `shipments.NewLifecycle()` machine and injects that
same instance into the authorized operation and `shipmentpostgres.New(pool,
machine)`. The operation loads a tenant-scoped snapshot, verifies current access,
and calls `shipments.DispatchLoaded(ctx, machine, writer, snapshot)`. The consumer
port accepts only `state.Evaluated[shipments.Status, shipments.Event]`. The writer
rejects zero or foreign-machine tokens before accessing PostgreSQL. There is no
public replacement-record write method or arbitrary write callback.

Apply `Schema` explicitly in application migrations; construction never executes
DDL. The concrete writer owns its transaction and sqlc queries: it atomically
matches tenant ID, shipment ID, source status and expected version, updates status
and version, then inserts dispatch intent in the same transaction. Missing/stale
snapshots and replay fail with `ErrConflict`. Outbox failure rolls back the state
update. The outbox row is this small example's audit record; a worker owns actual
delivery. An uncertain commit error must be reconciled instead of blindly retried.

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

`make database` requires its URL, fails on skipped or missing required cases, and
runs the real SQL tests with race detection. Ordinary tests explicitly skip SQL
cases without the URL; a general test pass is not database evidence. From the
Core repository root, `scripts/postgres-check.sh` creates a disposable PostgreSQL
17.6 container with a dynamic loopback port, runs both established adapter cases
and this reference consumer, then removes the container. It never connects to an
existing database. The schema-isolated tests cover all binding predicates, zero
and foreign-machine decisions, concurrent commits, replay, tenant isolation and
rollback when outbox insertion fails. Run
`PLUTO_CORE_SQL_MUTATION=1 scripts/postgres-check.sh` from the Core root for six
real-SQL mutations covering each CAS predicate, using the pool outside the
transaction, and omitting commit. Build failures, timeouts and missing databases
fail the mutation command rather than count as killed mutations. Each mutant runs
only its exact relevant test/subtest and must emit its intended business
assertion, with no unrelated failure output. Pristine real-SQL controls run before
and after the mutations. The evidence classifier runs self-tests rejecting
connection/setup errors, SQL errors, build failures, timeouts, absent cases and
wrong assertions before using the database.
`TestRequiredDatabaseGate` proves the
strict command rejects a missing URL.

Regenerate fixed typed queries with the pinned generator:

```sh
GOWORK=off go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.29.0 generate
```
