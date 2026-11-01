-- name: CommitDispatch :execrows
UPDATE shipments SET status = sqlc.arg(next_status), version = version + 1
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id)
  AND status = sqlc.arg(previous_status) AND version = sqlc.arg(expected_version);

-- name: InsertDispatchIntent :exec
INSERT INTO shipment_dispatch_outbox (tenant_id, shipment_id, version, event)
VALUES ($1, $2, $3, $4);

-- name: ReadVisibleShipment :one
SELECT s.id, s.tenant_id, s.status, s.version, s.address_verified FROM shipments s
JOIN shipment_visibility v ON v.tenant_id=s.tenant_id AND v.shipment_id=s.id
WHERE s.tenant_id=$1 AND s.id=$2 AND v.subject_id=$3;

-- name: ListVisibleShipments :many
SELECT s.id, s.status, s.version FROM shipments s
JOIN shipment_visibility v ON v.tenant_id=s.tenant_id AND v.shipment_id=s.id
WHERE s.tenant_id=$1 AND v.subject_id=$2 AND s.id > $3
ORDER BY s.id ASC LIMIT $4;

-- name: MayViewShipment :one
SELECT EXISTS(SELECT 1 FROM shipment_visibility
WHERE tenant_id=$1 AND shipment_id=$2 AND subject_id=$3);

-- name: CommitVisibleDispatch :execrows
UPDATE shipments s SET status = sqlc.arg(next_status), version = version + 1
WHERE s.tenant_id = sqlc.arg(tenant_id) AND s.id = sqlc.arg(id)
 AND s.status = sqlc.arg(previous_status) AND s.version = sqlc.arg(expected_version)
 AND EXISTS (SELECT 1 FROM shipment_visibility v
 WHERE v.tenant_id=s.tenant_id AND v.shipment_id=s.id
 AND v.subject_id=sqlc.arg(subject_id));

-- name: InsertAuthorizedDispatchAudit :exec
INSERT INTO shipment_dispatch_outbox (tenant_id, shipment_id, version, event, actor_id)
VALUES ($1, $2, $3, $4, $5);
