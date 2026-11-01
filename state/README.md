# Explicit business lifecycles

Review the purchase lifecycle in `examples/purchases/lifecycle.go`, then
the operation in `approve.go`. Install Core v0.4.0 or later.

The intended import is `github.com/plutoframework/pluto-core/state`.

One immutable machine describes many entities. Entities persist their own
current state. `Next(current, event, facts)` returns a proposed next state;
it never updates a record or executes an effect. On failure it returns the
unchanged input state and an error. Unknown states/events and undeclared
transitions fail closed. Duplicate state/event rules fail at construction.

Definitions live in Go, alongside business code. Named string types keep
states and events distinct. Facts are a value snapshot from verified identity
and loaded records. Named guards return application errors, preserved through
`errors.Is` together with `state.ErrGuard`. Guards must be pure; Go cannot
prevent a closure from performing I/O or capturing mutable external state.

Authorization remains in the operation. Persistence must atomically compare
tenant, resource ID, prior state and version, update the record, and write the
audit/outbox entries. Any write affecting a guard must advance the version.
Rules depending on other rows need transaction isolation, locks, or database
constraints appropriate to those rows; one entity's version is not enough.
The example repository interface documents this contract but does not implement
a database adapter. The machine is not a concurrency or authorization boundary
by itself. Existing jobs handle delivery after commit.

Outside this package: initial-state ownership, automatic
callbacks, global registration, reflection, timers, hierarchical states, durable
execution, and a separate DSL. Explicit self-transitions are allowed. A state
with no outgoing transitions is terminal. Business event names do not imply
event sourcing.

```sh
go test -race ./state ./examples/purchases
```

## Bound decisions (unpublished)

The additive `Binding`, `Evaluated`, `Machine.Evaluate` and `Machine.Validate`
APIs are local, unpublished work; they are not available in v0.4.0.
`Next` and existing consumers remain compatible. See `evaluated_test.go` for a
consumer-owned restricted write port and an atomic in-memory comparison fixture.
`examples/shipments` demonstrates an additive domain/operation write contract;
the
existing purchase example retains its compatible `Next` API.

```go
snapshot := state.Binding[Status]{
    Scope: tenantID, Entity: purchaseID, State: status, Version: version,
}
change, err := lifecycle.Evaluate(snapshot, Approve, facts)
// Propagate err before calling a restricted Commit(ctx, change) write port.
```

The adapter must call `lifecycle.Validate(change, currentSnapshot)` using the
same machine instance injected into the domain evaluation path. An independently
constructed machine, even with identical definitions, is rejected. Private
fields
prevent external callers from constructing valid tokens by composite literal.
Zero tokens and failed evaluations are invalid. The binding
includes scope, entity ID, source state and nonnegative expected version;
`Binding()` , `Event()` and `To()` return copies for a validated adapter's SQL
and
audit work. Scope and entity must be nonempty; single-tenant applications must
choose a stable explicit scope such as `single-tenant`.

The write port must accept the evaluated token instead of a publicly mutable
replacement record. Its database transaction must atomically compare all binding
fields, update the version, and write required audit/outbox work. A prior
`Validate` against a loaded record alone is insufficient. The concurrent fixture
shows this comparison contract in memory, not a database guarantee. Tokens may
be
copied and replayed; they are neither consumed nor expired. Machine provenance
is not current authorization, and guards are not rerun by validation. Private
entity state, controlled initial construction/rehydration and enforced consumer
adoption remain application and tooling responsibilities.

For concrete PostgreSQL CAS and transactional outbox proof, see the unpublished
nested consumer module `examples/shipments/postgres` . Its strict database
command
requires real execution and rejects missing or skipped required cases. This is
one adopted application write path, not automatic enforcement for every Core
consumer or a generic ORM.

Product documentation for this unpublished contract is maintained in the
[architecture companion](https://github.com/plutoframework/pluto-site/pull/5).
Every guard-relevant mutation must advance the persisted version. Validate
checks the evaluated snapshot and machine; authorization and atomic database
comparison remain the consumer's responsibility.
