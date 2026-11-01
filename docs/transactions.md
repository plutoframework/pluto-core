# Atomic business completion

The generic HTTP capture adapter does not expose a transaction to domain code.
For an atomic operation use the lower-level claim primitives in a thin transport
or application adapter. Keep the domain operation free of HTTP/pgx dependencies.

```go
// Auth, current permission/feature checks, trusted tenant resolution and bounded
// request fingerprinting precede this block. Claim scope uses verified identity.
replay, err := durable.Begin(
	ctx,
	claim,
) // durable is New(pool); committed claim
if err != nil {
	return err
} // pending/conflict: do not execute
if replay != nil {
	return writeReplay(replay)
}

tx, err := pool.Begin(ctx)
if err != nil {
	return err
}
defer tx.Rollback(context.Background())

// Use typed app sqlc queries bound to this SAME tx; preserve tenant predicates.
// Insert the business write and its audit. Construct bounded JSON response bytes.
response, err := mutateAndAudit(
	ctx,
	appQueries.WithTx(tx),
	principal,
	input,
)
if err != nil {
	return err
} // retained pending claim; reconcile deliberately
completion, err := postgres.New(tx)
if err != nil {
	return err
}
if err := completion.Complete(ctx, claim, response); err != nil {
	return err
}
if err := tx.Commit(ctx); err != nil {
	return err
} // uncertain: never rerun
return writeReplay(&response)
```

A rollback leaves neither mutation nor recorded completion, while the independent
claim remains pending. An operator can verify rollback before releasing it. A
commit publishes mutation, audit and completion together, so a retry can replay.
Never complete using the pool while the business tx is uncommitted. Never place
an external side effect in a transaction and infer rollback undid it. Integration
tests in `stores/postgres` exercise this pattern with real PostgreSQL.

## Evaluated lifecycle writes (unpublished reference consumer)

`examples/shipments/postgres` is a separate local-candidate consumer module, not
an ORM or a production generic adapter. Its restricted dispatch port requires an
opaque evaluated transition. The concrete writer validates the owning machine,
then owns a PostgreSQL transaction whose SQL matches scope, entity, source state
and version before updating state and inserting outbox/audit intent. Tests use a
real isolated database to reject stale/replayed decisions and prove outbox
failure rolls back the state update. This demonstrates one adopted write path;
it does not make all existing application writes mandatory or authorized.

Run `scripts/postgres-check.sh` for disposable-container evidence across the
existing PostgreSQL adapter and the reference consumer. The consumer's strict
`make database` command fails without its explicit database URL and rejects
skipped or absent required SQL cases. Its local Core replacement is intentional
unpublished development wiring, not release-pin compatibility evidence.

The checked-in Core workflow also invokes that strict disposable database command
with SQL mutations enabled. This is workflow wiring, not verification of hosted
branch protection or a claim that a remote CI run has executed. Ordinary local
checks can still skip SQL when no database URL is configured; strict evidence
comes from the explicit command and its required-case assertions.
