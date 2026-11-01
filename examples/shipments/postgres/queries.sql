-- name: CommitDispatch :execrows
UPDATE shipments SET status = sqlc.arg(next_status), version = version + 1
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id)
  AND status = sqlc.arg(previous_status) AND version = sqlc.arg(expected_version);

-- name: InsertDispatchIntent :exec
INSERT INTO shipment_dispatch_outbox (tenant_id, shipment_id, version, event)
VALUES ($1, $2, $3, $4);
