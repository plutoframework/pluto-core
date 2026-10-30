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
