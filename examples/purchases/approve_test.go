package purchases_test

import (
	"context"
	"errors"
	"testing"

	"github.com/plutoframework/pluto-core/examples/purchases"
)

type authorization struct{ err error }

func (a authorization) Require(
	context.Context,
	purchases.Principal,
	string,
) error {
	return a.err
}

type repository struct {
	purchase    purchases.Purchase
	commitError error
	reads       int
	changes     []purchases.Change
}

func (r *repository) Purchase(
	context.Context,
	string,
	string,
) (purchases.Purchase, error) {
	r.reads++
	return r.purchase, nil
}

func (r *repository) Commit(_ context.Context, change purchases.Change) error {
	r.changes = append(r.changes, change)
	return r.commitError
}

func TestApproveOperation(t *testing.T) {
	for _, scenario := range []string{"approve", "denied", "self", "wrong_state", "wrong_tenant", "conflict"} {
		t.Run(scenario, func(t *testing.T) {
			failure := errors.New("authorization or persistence rejection")
			auth := authorization{}
			repo := &repository{purchase: purchases.Purchase{
				ID: "purchase-1", TenantID: "tenant-1", CreatorID: "creator",
				Status: purchases.Submitted, Version: 7, AmountCents: 125_000,
			}}
			actor := purchases.Principal{
				ActorID:  "approver",
				TenantID: "tenant-1",
			}

			switch scenario {
			case "denied":
				auth.err = failure
			case "self":
				actor.ActorID = "creator"
			case "wrong_state":
				repo.purchase.Status = purchases.Draft
			case "wrong_tenant":
				repo.purchase.TenantID = "another-tenant"
			case "conflict":
				repo.commitError = failure
			}

			service, err := purchases.NewService(auth, repo)
			if err != nil {
				t.Fatal(err)
			}

			err = service.Approve(context.Background(), actor, "purchase-1")
			if (err == nil) != (scenario == "approve") {
				t.Fatalf("unexpected outcome: %v", err)
			}

			if scenario == "approve" || scenario == "conflict" {
				if len(repo.changes) != 1 {
					t.Fatal("one atomic commit expected")
				}

				change := repo.changes[0]
				if change.ExpectedVersion != 7 ||
					change.From != purchases.Submitted ||
					change.To != purchases.Approved ||
					change.TenantID != actor.TenantID {
					t.Fatal("persistence preconditions were lost")
				}
			} else if len(repo.changes) != 0 {
				t.Fatal("rejected operation reached persistence")
			}

			if scenario == "denied" && repo.reads != 0 {
				t.Fatal("unauthorized operation read data")
			}

			if scenario == "conflict" && !errors.Is(err, failure) {
				t.Fatal("persistence conflict must reach caller")
			}
		})
	}
}
