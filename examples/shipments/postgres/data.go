//go:build plutocandidate

package shipmentpostgres

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/plutoframework/pluto-core/cache"
	"github.com/plutoframework/pluto-core/examples/shipments"
	"github.com/plutoframework/pluto-core/examples/shipments/postgres/contract"
	"github.com/plutoframework/pluto-core/examples/shipments/postgres/internal/db"
	"github.com/plutoframework/pluto-core/idempotency"
	"github.com/plutoframework/pluto-core/state"
	idempostgres "github.com/plutoframework/pluto-core/stores/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
	jobs "github.com/plutoframework/pluto-jobs"
	jobspostgres "github.com/plutoframework/pluto-jobs/stores/postgres"
	identitygate "github.com/plutoframework/pluto-rbac/identity"
)

// DeliverySchema is the candidate Jobs schema snapshot for explicit application
// migration. Data construction performs no DDL.
//
//go:embed jobs-schema.sql
var DeliverySchema string

var ErrAttempt = errors.New("invalid shipment dispatch attempt")

type DispatchResult = contract.Result

type DispatchNotice struct {
	ShipmentID string `json:"shipment_id"`
}

// NewDispatchEffect is the typed background contract. A delivery operation must
// establish current authority; a stored tenant and job ID are metadata only.
func NewDispatchEffect() (*jobs.Definition[DispatchNotice], error) {
	return jobs.NewDefinition(
		"shipment.dispatched",
		func(value DispatchNotice) error {
			if value.ShipmentID == "" || len(value.ShipmentID) > 128 {
				return shipments.ErrInput
			}

			return nil
		},
	)
}

type DataConfig struct {
	App         string
	Environment string
	CacheTTL    time.Duration
}

// Data is a concrete application boundary, not a generic transaction callback.
// Each public operation uses the injected identity gate before cache or SQL.
// Composition must inject the exact same gate and machine used by operations.
type Data struct {
	pool    *pgxpool.Pool
	gate    *identitygate.Gate
	machine *state.Machine[shipments.Status, shipments.Event, bool]
	cache   cache.Store
	loader  *cache.Loader[shipments.Snapshot]
	config  DataConfig
	effect  *jobs.Definition[DispatchNotice]
}

func NewData(
	pool *pgxpool.Pool,
	gate *identitygate.Gate,
	machine *state.Machine[shipments.Status, shipments.Event, bool],
	store cache.Store,
	config DataConfig,
) (*Data, error) {
	if pool == nil || gate == nil || machine == nil || store == nil ||
		config.App == "" ||
		config.Environment == "" ||
		config.CacheTTL <= 0 ||
		config.CacheTTL > time.Minute {
		return nil, ErrDependencies
	}

	loader, err := cache.NewLoader[shipments.Snapshot](
		store,
		cache.DefaultLoaderOptions(),
	)
	if err != nil {
		return nil, err
	}

	effect, err := NewDispatchEffect()
	if err != nil {
		return nil, err
	}

	return &Data{
		pool:    pool,
		gate:    gate,
		machine: machine,
		cache:   store,
		loader:  loader,
		config:  config,
		effect:  effect,
	}, nil
}

// DispatchAttempt carries a claimed logical command, bound to this adapter,
// verified actor/tenant and normalized input. It has no public valid constructor.
// Copies share the same durable claim; only SQL determines commit/replay.
type dispatchAttempt struct {
	owner   *Data
	input   shipments.DispatchInput
	subject string
	tenant  string
	claim   idempotency.Claim
	job     jobs.Request
	replay  *DispatchResult
}

func (a *dispatchAttempt) Replayed() (DispatchResult, bool) {
	if a == nil || a.replay == nil {
		return DispatchResult{}, false
	}

	return *a.replay, true
}

// StartDispatch is required before loading/evaluating the lifecycle, so a retry
// can replay a committed result after the entity has already changed state.
// Authorization and current resource visibility precede even a replay. Any
// subsequent failure leaves a pending claim requiring deliberate reconciliation.
func (d *Data) StartDispatch(
	ctx context.Context,
	execution *identitygate.Execution,
	input shipments.DispatchInput,
) (contract.Attempt, error) {
	if d == nil || d.gate == nil || execution == nil {
		return nil, ErrDependencies
	}

	if err := input.Validate(); err != nil {
		return nil, err
	}

	var attempt *dispatchAttempt

	err := d.gate.Use(
		ctx,
		execution,
		execution.TenantID(),
		input.ID(),
		func(ctx context.Context) error {
			if err := d.requireVisible(ctx, execution, input.ID()); err != nil {
				return err
			}

			fingerprint := sha256.Sum256(
				[]byte("shipment.dispatch\x00" + input.ID()),
			)

			claim, err := idempotency.NewClaim(
				idempotency.Scope{
					Subject:   execution.SubjectID(),
					Tenant:    execution.TenantID(),
					Operation: "shipment.dispatch",
				},
				input.Key(),
				hex.EncodeToString(fingerprint[:]),
				time.Now(),
				time.Hour,
			)
			if err != nil {
				return err
			}

			durable, err := idempostgres.New(d.pool)
			if err != nil {
				return err
			}

			replay, err := durable.Begin(ctx, claim)
			if err != nil {
				return err
			}

			attempt = &dispatchAttempt{
				owner:   d,
				input:   input,
				subject: execution.SubjectID(),
				tenant:  execution.TenantID(),
				claim:   claim,
			}

			if replay != nil {
				var result DispatchResult
				if err := json.Unmarshal(replay.Body, &result); err != nil {
					return ErrAttempt
				}

				if result.ShipmentID != input.ID() ||
					result.Status != shipments.Dispatched ||
					result.JobID == "" {
					return ErrAttempt
				}

				attempt.replay = &result

				return nil
			}

			request, err := d.effect.Prepare(
				DispatchNotice{ShipmentID: input.ID()},
				jobs.Options{TenantID: &attempt.tenant, MaxAttempts: 5},
			)
			if err != nil {
				return err
			}

			attempt.job = request

			return nil
		},
	)
	if err != nil {
		return nil, err
	}

	return attempt, nil
}

// CommitDispatch atomically commits the fixed shipment mutation, its audit,
// typed Jobs outbox request and idempotency completion. It owns the transaction;
// it never runs a caller-provided SQL or external-effect callback inside it.
func (d *Data) CommitDispatch(
	ctx context.Context,
	execution *identitygate.Execution,
	rawAttempt contract.Attempt,
	change state.Evaluated[shipments.Status, shipments.Event],
) (DispatchResult, error) {
	attempt, ok := rawAttempt.(*dispatchAttempt)
	if !ok {
		return DispatchResult{}, ErrAttempt
	}

	if d == nil || d.gate == nil || execution == nil || attempt == nil ||
		attempt.owner != d ||
		attempt.replay != nil {
		return DispatchResult{}, ErrAttempt
	}

	if attempt.subject != execution.SubjectID() ||
		attempt.tenant != execution.TenantID() {
		return DispatchResult{}, ErrAttempt
	}

	binding := change.Binding()
	if err := d.machine.Validate(change, binding); err != nil {
		return DispatchResult{}, err
	}

	if binding.Scope != attempt.tenant || binding.Entity != attempt.input.ID() {
		return DispatchResult{}, ErrAttempt
	}

	if change.Event() != shipments.Dispatch ||
		change.To() != shipments.Dispatched ||
		binding.State != shipments.Packed ||
		binding.Version == math.MaxInt64 {
		return DispatchResult{}, ErrDispatch
	}

	var result DispatchResult

	err := d.gate.Use(
		ctx,
		execution,
		attempt.tenant,
		attempt.input.ID(),
		func(ctx context.Context) error {
			tx, err := d.pool.Begin(ctx)
			if err != nil {
				return err
			}

			defer func() {
				cleanup, cancel := context.WithTimeout(
					context.Background(),
					5*time.Second,
				)
				defer cancel()

				_ = tx.Rollback(cleanup)
			}()

			queries := db.New(tx)

			changed, err := queries.CommitVisibleDispatch(
				ctx,
				db.CommitVisibleDispatchParams{
					TenantID:        binding.Scope,
					ID:              binding.Entity,
					PreviousStatus:  string(binding.State),
					ExpectedVersion: binding.Version,
					NextStatus:      string(change.To()),
					SubjectID:       attempt.subject,
				},
			)
			if err != nil {
				return err
			}

			if changed != 1 {
				return ErrConflict
			}

			if err := queries.InsertAuthorizedDispatchAudit(
				ctx,
				db.InsertAuthorizedDispatchAuditParams{
					TenantID:   binding.Scope,
					ShipmentID: binding.Entity,
					Version:    binding.Version + 1,
					Event:      string(change.Event()),
					ActorID:    attempt.subject,
				},
			); err != nil {
				return err
			}

			delivery, err := jobspostgres.New(d.pool)
			if err != nil {
				return err
			}

			if err := delivery.EnqueueTx(ctx, tx, attempt.job); err != nil {
				return err
			}

			result = DispatchResult{
				ShipmentID: binding.Entity,
				Status:     change.To(),
				Version:    binding.Version + 1,
				JobID:      attempt.job.ID,
			}

			body, err := json.Marshal(result)
			if err != nil {
				return err
			}

			completion, err := idempostgres.New(tx)
			if err != nil {
				return err
			}

			if err := completion.Complete(
				ctx,
				attempt.claim,
				idempotency.Response{
					Status:      200,
					ContentType: "application/json",
					Body:        body,
				},
			); err != nil {
				return err
			}

			return tx.Commit(ctx)
		},
	)
	if err != nil {
		return DispatchResult{}, err
	}

	// Cache is non-authoritative and bounded by TTL. Invalidation failure cannot
	// undo a committed transaction or turn it into permission to write again.
	if key, err := d.snapshotKey(execution, attempt.input.ID()); err == nil {
		_ = d.cache.Delete(ctx, key)
	}

	return result, nil
}

var (
	_ contract.DispatchData = (*Data)(nil)
	_ contract.ListData     = (*Data)(nil)
	_ contract.ExportData   = (*Data)(nil)
)

var _ contract.DisplayData = (*Data)(nil)
