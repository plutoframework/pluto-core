//go:build plutocandidate

package shipmentpostgres_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/plutoframework/pluto-core/cache"
	cachememory "github.com/plutoframework/pluto-core/cache/memory"
	"github.com/plutoframework/pluto-core/examples/shipments"
	shipmentpostgres "github.com/plutoframework/pluto-core/examples/shipments/postgres"
	"github.com/plutoframework/pluto-core/examples/shipments/postgres/contract"
	"github.com/plutoframework/pluto-core/examples/shipments/postgres/operations"
	"github.com/plutoframework/pluto-core/idempotency"
	"github.com/plutoframework/pluto-core/pagination"
	"github.com/plutoframework/pluto-core/state"
	idempostgres "github.com/plutoframework/pluto-core/stores/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
	auth "github.com/plutoframework/pluto-auth"
	authmemory "github.com/plutoframework/pluto-auth/stores/memory"
	rbac "github.com/plutoframework/pluto-rbac"
	identitygate "github.com/plutoframework/pluto-rbac/identity"
)

type forgedAttempt struct{}

func (forgedAttempt) Replayed() (contract.Result, bool) { return contract.Result{}, false }

type observedCache struct {
	cache.Store
	gets atomic.Int32
}

func (s *observedCache) Get(
	ctx context.Context,
	key string,
) ([]byte, bool, error) {
	s.gets.Add(1)

	return s.Store.Get(ctx, key)
}

type dataFixture struct {
	manager    *auth.Manager
	tokens     map[string]string
	gate       *identitygate.Gate
	data       *shipmentpostgres.Data
	machine    *state.Machine[shipments.Status, shipments.Event, bool]
	cache      *observedCache
	membership bool
	resource   bool
}

func newDataFixture(t *testing.T, pool *pgxpool.Pool) *dataFixture {
	t.Helper()

	f := &dataFixture{
		tokens:     map[string]string{},
		membership: true,
		resource:   true,
		machine:    owningMachine(t),
	}

	sessions, err := authmemory.New(20)
	if err != nil {
		t.Fatal(err)
	}

	f.manager, err = auth.New(
		sessions,
		auth.Config{TTL: time.Hour, Clock: time.Now},
	)
	if err != nil {
		t.Fatal(err)
	}

	for _, actor := range []string{"alice", "bob"} {
		issued, err := f.manager.Start(t.Context(), actor)
		if err != nil {
			t.Fatal(err)
		}

		f.tokens[actor] = issued.Token
	}

	policy, err := rbac.New(
		rbac.Definition{
			Permissions: []rbac.Permission{
				{
					ID:          "shipments",
					Name:        "Shipments",
					Description: "Access shipments",
				},
			},
			Roles: []rbac.Role{
				{
					ID:          "operator",
					Name:        "Operator",
					Description: "Operate shipments",
					Permissions: []rbac.PermissionID{"shipments"},
				},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	authorizer, err := rbac.NewAuthorizer(
		policy,
		rbac.RoleResolverFunc(
			func(context.Context, string, rbac.Scope) ([]rbac.RoleID, error) {
				return []rbac.RoleID{"operator"}, nil
			},
		),
		rbac.Options{RequireTenant: true},
	)
	if err != nil {
		t.Fatal(err)
	}

	required, err := authorizer.Require("shipments")
	if err != nil {
		t.Fatal(err)
	}

	resource, err := required.WithResourcePolicy(
		rbac.ResourcePolicyFunc(
			func(_ context.Context, request rbac.Request, key string) error {
				if !f.resource || key == "" ||
					request.Scope.TenantID != "tenant-a" {
					return rbac.ErrForbidden
				}

				return nil
			},
		),
	)
	if err != nil {
		t.Fatal(err)
	}

	f.gate, err = identitygate.New(
		f.manager,
		resource,
		identitygate.MembershipVerifierFunc(
			func(_ context.Context, subject, tenant string) error {
				if !f.membership || tenant != "tenant-a" ||
					(subject != "alice" && subject != "bob") {
					return rbac.ErrForbidden
				}

				return nil
			},
		),
	)
	if err != nil {
		t.Fatal(err)
	}

	memory, err := cachememory.New(cachememory.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}

	f.cache = &observedCache{Store: memory}

	f.data, err = shipmentpostgres.NewData(
		pool,
		f.gate,
		f.machine,
		f.cache,
		shipmentpostgres.DataConfig{
			App:         "reference",
			Environment: "test",
			CacheTTL:    time.Minute,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(t.Context(), idempostgres.Schema); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(
		t.Context(),
		shipmentpostgres.DeliverySchema,
	); err != nil {
		t.Fatal(err)
	}

	return f
}

func (f *dataFixture) within(
	t *testing.T,
	actor, resource string,
	run func(context.Context, *identitygate.Execution) error,
) error {
	t.Helper()

	return f.manager.WithIdentity(
		t.Context(),
		f.tokens[actor],
		func(ctx context.Context, identity *auth.Identity) error {
			return f.gate.Within(ctx, identity, "tenant-a", resource, run)
		},
	)
}

func grant(t *testing.T, pool *pgxpool.Pool, id, actor string) {
	t.Helper()

	if _, err := pool.Exec(
		t.Context(),
		"INSERT INTO shipment_visibility (tenant_id,shipment_id,subject_id) VALUES ('tenant-a',$1,$2)",
		id,
		actor,
	); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresAuthorizedScopedCache(t *testing.T) {
	pool := isolated(t)
	f := newDataFixture(t, pool)
	snapshot := loaded()
	seed(t, pool, snapshot)
	grant(t, pool, snapshot.ID, "alice")
	grant(t, pool, snapshot.ID, "bob")

	input, _ := shipments.NewDispatchInput(snapshot.ID, "unauthorized")
	if _, err := operations.Dispatch(
		t.Context(),
		nil,
		f.data,
		f.machine,
		input,
	); err == nil {
		t.Fatal("direct operation without verified execution succeeded")
	}

	if f.cache.gets.Load() != 0 {
		t.Fatal("unverified operation touched cache")
	}

	var escaped *identitygate.Execution

	err := f.within(
		t,
		"alice",
		snapshot.ID,
		func(ctx context.Context, execution *identitygate.Execution) error {
			escaped = execution

			first, err := f.data.Load(ctx, execution, snapshot.ID)
			if err != nil {
				return err
			}

			if first.Version != 7 {
				return fmt.Errorf("snapshot lost")
			}

			count := f.cache.gets.Load()

			f.resource = false
			if _, err := f.data.Load(
				ctx,
				execution,
				snapshot.ID,
			); !errors.Is(
				err,
				rbac.ErrForbidden,
			) {
				return fmt.Errorf("resource denied cache access: %v", err)
			}

			f.resource = true

			f.membership = false
			if _, err := f.data.Load(
				ctx,
				execution,
				snapshot.ID,
			); !errors.Is(
				err,
				rbac.ErrForbidden,
			) {
				return fmt.Errorf("membership denied cache access: %v", err)
			}

			f.membership = true
			if f.cache.gets.Load() != count {
				return fmt.Errorf("denied authorization reached cache")
			}

			if _, err := pool.Exec(
				ctx,
				"DELETE FROM shipment_visibility WHERE subject_id='alice'",
			); err != nil {
				return err
			}

			if _, err := f.data.Load(
				ctx,
				execution,
				snapshot.ID,
			); !errors.Is(
				err,
				shipmentpostgres.ErrVisibility,
			) {
				return fmt.Errorf("revoked row visibility hit cache: %v", err)
			}

			if f.cache.gets.Load() != count {
				return fmt.Errorf("denied visibility reached cache")
			}

			grant(t, pool, snapshot.ID, "alice")

			if _, err := pool.Exec(
				ctx,
				"UPDATE shipments SET version=8",
			); err != nil {
				return err
			}

			cached, err := f.data.Load(ctx, execution, snapshot.ID)
			if err != nil {
				return err
			}

			if cached.Version != 7 {
				return fmt.Errorf("expected actual cached snapshot")
			}

			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	count := f.cache.gets.Load()
	if _, err := f.data.Load(t.Context(), escaped, snapshot.ID); err == nil {
		t.Fatal("escaped execution reached cache")
	}

	if f.cache.gets.Load() != count {
		t.Fatal("escaped execution touched cache")
	}

	err = f.within(
		t,
		"bob",
		snapshot.ID,
		func(ctx context.Context, execution *identitygate.Execution) error {
			value, err := f.data.Load(ctx, execution, snapshot.ID)
			if err != nil {
				return err
			}

			if value.Version != 8 {
				return fmt.Errorf("actor cache key collision")
			}

			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	err = f.within(
		t,
		"alice",
		shipmentpostgres.CollectionResource,
		func(ctx context.Context, execution *identitygate.Execution) error {
			if _, err := f.data.Load(ctx, execution, snapshot.ID); err == nil {
				return fmt.Errorf("collection execution authorized entity read")
			}

			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestPostgresVisiblePaginationAndBoundedExport(t *testing.T) {
	pool := isolated(t)
	f := newDataFixture(t, pool)

	for index := 0; index < 5; index++ {
		snapshot := loaded()
		snapshot.ID = fmt.Sprintf("shipment-%02d", index)
		seed(t, pool, snapshot)

		if index == 1 || index == 3 || index == 4 {
			grant(t, pool, snapshot.ID, "alice")
		}

		grant(t, pool, snapshot.ID, "bob")
	}

	var cursor string

	err := f.within(
		t,
		"alice",
		shipmentpostgres.CollectionResource,
		func(ctx context.Context, execution *identitygate.Execution) error {
			input, _ := shipments.NewListInput(2, "")

			page, err := f.data.List(ctx, execution, input)
			if err != nil {
				return err
			}

			if len(page.Data) != 2 || page.Data[0].ID != "shipment-01" ||
				page.Data[1].ID != "shipment-03" ||
				!page.Page.HasMore {
				return errors.New("SQL_VISIBILITY_BEFORE_LIMIT")
			}

			cursor = page.Page.NextCursor
			next, _ := shipments.NewListInput(2, cursor)

			page, err = f.data.List(ctx, execution, next)
			if err != nil {
				return err
			}

			if len(page.Data) != 1 || page.Data[0].ID != "shipment-04" ||
				page.Page.HasMore {
				return fmt.Errorf("visible continuation lost")
			}

			export, _ := shipments.NewExportInput(3)

			rows, err := f.data.Export(ctx, execution, export)
			if err != nil {
				return err
			}

			if len(rows) != 3 {
				return fmt.Errorf("export visibility lost")
			}

			bounded, _ := shipments.NewExportInput(2)
			if rows, err := f.data.Export(
				ctx,
				execution,
				bounded,
			); !errors.Is(err, shipmentpostgres.ErrExportLimit) ||
				rows != nil {
				return fmt.Errorf(
					"unbounded/truncated export accepted: %v",
					err,
				)
			}

			f.membership = false
			if _, err := f.data.List(
				ctx,
				execution,
				next,
			); !errors.Is(
				err,
				rbac.ErrForbidden,
			) {
				return fmt.Errorf("page reused stale authority: %v", err)
			}

			f.membership = true

			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	err = f.within(
		t,
		"bob",
		shipmentpostgres.CollectionResource,
		func(ctx context.Context, execution *identitygate.Execution) error {
			input, _ := shipments.NewListInput(2, cursor)

			_, err := f.data.List(ctx, execution, input)
			if !errors.Is(err, pagination.ErrInvalid) {
				return fmt.Errorf("cross-actor cursor accepted: %v", err)
			}

			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestPostgresDeclaredAtomicDispatch(t *testing.T) {
	for _, failure := range []string{"none", "audit", "delivery", "completion", "commit"} {
		t.Run(failure, func(t *testing.T) {
			pool := isolated(t)
			f := newDataFixture(t, pool)
			snapshot := loaded()
			seed(t, pool, snapshot)
			grant(t, pool, snapshot.ID, "alice")

			input, err := shipments.NewDispatchInput(
				" shipment-1 ",
				"logical-1",
			)
			if err != nil {
				t.Fatal(err)
			}

			var (
				first   shipmentpostgres.DispatchResult
				attempt contract.Attempt
			)

			err = f.within(
				t,
				"alice",
				input.ID(),
				func(ctx context.Context, execution *identitygate.Execution) error {
					var err error

					attempt, err = f.data.StartDispatch(ctx, execution, input)
					if err != nil {
						return err
					}

					if _, replayed := attempt.Replayed(); replayed {
						return fmt.Errorf("fresh command replayed")
					}

					loaded, err := f.data.Load(ctx, execution, input.ID())
					if err != nil {
						return err
					}

					change := decision(t, f.machine, loaded)
					if _, err := f.data.CommitDispatch(
						ctx,
						execution,
						forgedAttempt{},
						change,
					); !errors.Is(
						err,
						shipmentpostgres.ErrAttempt,
					) {
						return fmt.Errorf("forged attempt accepted: %v", err)
					}

					foreign := decision(t, owningMachine(t), loaded)
					if _, err := f.data.CommitDispatch(
						ctx,
						execution,
						attempt,
						foreign,
					); !errors.Is(
						err,
						state.ErrMachine,
					) {
						return fmt.Errorf("foreign lifecycle accepted: %v", err)
					}

					switch failure {
					case "audit":
						_, err = pool.Exec(
							ctx,
							"ALTER TABLE shipment_dispatch_outbox ADD CONSTRAINT injected_failure CHECK(false) NOT VALID",
						)
					case "delivery":
						_, err = pool.Exec(
							ctx,
							"ALTER TABLE pluto_jobs ADD CONSTRAINT injected_failure CHECK(false) NOT VALID",
						)
					case "commit":
						_, err = pool.Exec(
							ctx,
							`CREATE FUNCTION injected_commit_failure() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'injected commit failure'; END$$;
 CREATE CONSTRAINT TRIGGER injected_commit_failure AFTER UPDATE ON shipments DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION injected_commit_failure()`,
						)
					case "completion":
						_, err = pool.Exec(
							ctx,
							"ALTER TABLE pluto_idempotency ADD CONSTRAINT injected_failure CHECK(NOT completed) NOT VALID",
						)
					}

					if err != nil {
						return err
					}

					first, err = f.data.CommitDispatch(
						ctx,
						execution,
						attempt,
						change,
					)
					if failure != "none" {
						if err == nil {
							return fmt.Errorf(
								"injected atomic participant failure accepted",
							)
						}

						assertRows(t, pool, snapshot, shipments.Packed, 7, 0)

						var jobs, completed int
						if err := pool.QueryRow(ctx, "SELECT count(*) FROM pluto_jobs").
							Scan(&jobs); err != nil {
							return err
						}

						if err := pool.QueryRow(ctx, "SELECT count(*) FROM pluto_idempotency WHERE completed").
							Scan(&completed); err != nil {
							return err
						}

						if jobs != 0 || completed != 0 {
							return fmt.Errorf(
								"partial transaction committed jobs=%d completed=%d",
								jobs,
								completed,
							)
						}

						if _, err := f.data.StartDispatch(
							ctx,
							execution,
							input,
						); !errors.Is(
							err,
							idempotency.ErrInProgress,
						) {
							return fmt.Errorf(
								"pending claim silently restarted: %v",
								err,
							)
						}

						return nil
					}

					if err != nil {
						return err
					}

					if first.Version != 8 || first.JobID == "" {
						return fmt.Errorf("committed result lost")
					}

					replay, err := f.data.StartDispatch(ctx, execution, input)
					if err != nil {
						return err
					}

					result, yes := replay.Replayed()
					if !yes || result != first {
						return fmt.Errorf("committed response was not replayed")
					}

					if _, err := f.data.CommitDispatch(
						ctx,
						execution,
						attempt,
						change,
					); !errors.Is(
						err,
						shipmentpostgres.ErrConflict,
					) {
						return fmt.Errorf(
							"same attempt replay committed: %v",
							err,
						)
					}

					return nil
				},
			)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
