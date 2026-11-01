-- Reference application migration; never run by the writer constructor.
CREATE TABLE shipments (
    tenant_id text NOT NULL,
    id text NOT NULL,
    status text NOT NULL CHECK (status IN ('packed', 'dispatched')),
    version bigint NOT NULL CHECK (version >= 0),
    address_verified boolean NOT NULL,
    PRIMARY KEY (tenant_id, id)
);
-- Durable dispatch intent doubles as this example's audit record. Delivery is
-- performed after commit by an independently authorized worker, not this writer.
CREATE TABLE shipment_dispatch_outbox (
    tenant_id text NOT NULL,
    shipment_id text NOT NULL,
    version bigint NOT NULL,
    event text NOT NULL CHECK (event = 'dispatch'),
    actor_id text NOT NULL DEFAULT '',
    PRIMARY KEY (tenant_id, shipment_id, version),
    FOREIGN KEY (tenant_id, shipment_id) REFERENCES shipments (tenant_id, id)
);

-- Application-owned visibility relation, checked before keyset pagination.
CREATE TABLE shipment_visibility (
    tenant_id text NOT NULL,
    shipment_id text NOT NULL,
    subject_id text NOT NULL,
    PRIMARY KEY (tenant_id, shipment_id, subject_id),
    FOREIGN KEY (tenant_id, shipment_id) REFERENCES shipments (tenant_id, id)
);
