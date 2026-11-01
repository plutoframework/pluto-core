//go:build plutocandidate

package shipmentpostgres

import (
	"context"
	"errors"
	"net/url"
	"strconv"

	"github.com/plutoframework/pluto-core/cache"
	"github.com/plutoframework/pluto-core/examples/shipments"
	"github.com/plutoframework/pluto-core/examples/shipments/postgres/contract"
	"github.com/plutoframework/pluto-core/examples/shipments/postgres/internal/db"
	"github.com/plutoframework/pluto-core/pagination"

	identitygate "github.com/plutoframework/pluto-rbac/identity"
)

const CollectionResource = "shipments"

var (
	ErrVisibility  = errors.New("shipment is not currently visible")
	ErrExportLimit = errors.New("shipment export exceeds requested bound")
)

type ShipmentSummary = contract.Summary

func (d *Data) snapshotKey(
	execution *identitygate.Execution,
	id string,
) (string, error) {
	return cache.Key(
		cache.Scope{
			App:         d.config.App,
			Environment: d.config.Environment,
			Namespace:   "shipment-snapshot",
			Version:     "v1",
			TenantID:    execution.TenantID(),
			SubjectID:   execution.SubjectID(),
		},
		id,
	)
}

func (d *Data) requireVisible(
	ctx context.Context,
	execution *identitygate.Execution,
	id string,
) error {
	visible, err := db.New(d.pool).
		MayViewShipment(ctx, db.MayViewShipmentParams{TenantID: execution.TenantID(), ShipmentID: id, SubjectID: execution.SubjectID()})
	if err != nil {
		return err
	}

	if !visible {
		return ErrVisibility
	}

	return nil
}

// Load reads authoritative guard facts, without consulting the display cache.
func (d *Data) Load(
	ctx context.Context,
	execution *identitygate.Execution,
	id string,
) (shipments.Snapshot, error) {
	return d.load(ctx, execution, id, false)
}

// Display is an authorized, scoped cached read. Its facts are never used by
// the transition operation; current visibility is still checked before each hit.
func (d *Data) Display(
	ctx context.Context,
	execution *identitygate.Execution,
	id string,
) (shipments.Snapshot, error) {
	return d.load(ctx, execution, id, true)
}

func (d *Data) load(
	ctx context.Context,
	execution *identitygate.Execution,
	id string,
	cached bool,
) (shipments.Snapshot, error) {
	if d == nil || d.gate == nil || execution == nil {
		return shipments.Snapshot{}, ErrDependencies
	}

	if id == "" || len(id) > 128 {
		return shipments.Snapshot{}, shipments.ErrInput
	}

	var value shipments.Snapshot

	err := d.gate.Use(
		ctx,
		execution,
		execution.TenantID(),
		id,
		func(ctx context.Context) error {
			if !cached {
				var err error

				value, err = d.readSnapshot(ctx, execution, id)

				return err
			}

			if err := d.requireVisible(ctx, execution, id); err != nil {
				return err
			}

			key, err := d.snapshotKey(execution, id)
			if err != nil {
				return err
			}

			value, err = d.loader.Remember(
				ctx,
				key,
				d.config.CacheTTL,
				func(ctx context.Context) (shipments.Snapshot, error) {
					return d.readSnapshot(ctx, execution, id)
				},
			)

			return err
		},
	)
	if err != nil {
		return shipments.Snapshot{}, err
	}

	if value.ID != id || value.TenantID != execution.TenantID() {
		return shipments.Snapshot{}, ErrVisibility
	}

	return value, nil
}

func (d *Data) readSnapshot(
	ctx context.Context,
	execution *identitygate.Execution,
	id string,
) (shipments.Snapshot, error) {
	record, err := db.New(d.pool).
		ReadVisibleShipment(ctx, db.ReadVisibleShipmentParams{TenantID: execution.TenantID(), ID: id, SubjectID: execution.SubjectID()})
	if err != nil {
		return shipments.Snapshot{}, err
	}

	return shipments.Snapshot{
		ID:              record.ID,
		TenantID:        record.TenantID,
		Status:          shipments.Status(record.Status),
		Version:         record.Version,
		AddressVerified: record.AddressVerified,
	}, nil
}

// List applies current authorization and SQL row visibility before keyset LIMIT.
// Its cursor binds current verified actor/tenant and the fixed visibility policy.
func (d *Data) List(
	ctx context.Context,
	execution *identitygate.Execution,
	input shipments.ListInput,
) (pagination.Page[ShipmentSummary], error) {
	if d == nil || d.gate == nil || execution == nil {
		return pagination.Page[ShipmentSummary]{}, ErrDependencies
	}

	if err := input.Validate(); err != nil {
		return pagination.Page[ShipmentSummary]{}, err
	}

	var page pagination.Page[ShipmentSummary]

	err := d.gate.Use(
		ctx,
		execution,
		execution.TenantID(),
		CollectionResource,
		func(ctx context.Context) error {
			query := url.Values{"limit": {strconv.Itoa(input.Limit())}}
			if input.Cursor() != "" {
				query.Set("cursor", input.Cursor())
			}

			binding := pagination.Binding(
				query,
				"shipments:list",
				execution.SubjectID(),
				execution.TenantID(),
				"visibility-v1",
			)

			request, err := pagination.Parse(query, binding)
			if err != nil {
				return err
			}

			rows, err := db.New(d.pool).
				ListVisibleShipments(ctx, db.ListVisibleShipmentsParams{TenantID: execution.TenantID(), SubjectID: execution.SubjectID(), ID: request.After.ID, Limit: int32(request.FetchLimit())})
			if err != nil {
				return err
			}

			values := make([]ShipmentSummary, 0, len(rows))
			for _, row := range rows {
				values = append(
					values,
					ShipmentSummary{
						ID:      row.ID,
						Status:  shipments.Status(row.Status),
						Version: row.Version,
					},
				)
			}

			page, err = pagination.Finish(
				request,
				values,
				func(value ShipmentSummary) pagination.Key { return pagination.Key{ID: value.ID} },
			)

			return err
		},
	)

	return page, err
}

// Export uses only the same visible bounded query as List, rechecking authority
// each page. It refuses a truncated result rather than exporting unbounded rows.
func (d *Data) Export(
	ctx context.Context,
	execution *identitygate.Execution,
	input shipments.ExportInput,
) ([]ShipmentSummary, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	rows := make([]ShipmentSummary, 0, input.Rows())
	cursor := ""

	for {
		limit := min(pagination.MaxLimit, input.Rows()-len(rows))

		list, err := shipments.NewListInput(limit, cursor)
		if err != nil {
			return nil, err
		}

		page, err := d.List(ctx, execution, list)
		if err != nil {
			return nil, err
		}

		rows = append(rows, page.Data...)
		if !page.Page.HasMore {
			return rows, nil
		}

		if len(rows) == input.Rows() {
			return nil, ErrExportLimit
		}

		cursor = page.Page.NextCursor
	}
}
