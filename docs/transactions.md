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
