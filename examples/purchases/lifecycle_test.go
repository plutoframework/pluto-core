package purchases_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/plutoframework/pluto-core/examples/purchases"
	"github.com/plutoframework/pluto-core/state"
)

func ExampleNewLifecycle() {
	lifecycle, err := purchases.NewLifecycle()
	if err != nil {
		panic(err)
	}

	next, err := lifecycle.Next(purchases.Submitted, purchases.Approve,
		purchases.Facts{
			ActorID:     "approver-1",
			CreatorID:   "requester-1",
			AmountCents: 125_000,
		},
	)
	fmt.Println(next, err)

	// Output: approved <nil>
}

func TestPurchaseRules(t *testing.T) {
	machine, err := purchases.NewLifecycle()
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		from  purchases.Status
		event purchases.Event
		actor string
		want  purchases.Status
		err   error
	}{
		{purchases.Draft, purchases.Submit, "creator", purchases.Submitted, nil},
		{purchases.Draft, purchases.Submit, "other", purchases.Draft, purchases.ErrCreatorRequired},
		{purchases.Submitted, purchases.Approve, "approver", purchases.Approved, nil},
		{purchases.Submitted, purchases.Reject, "approver", purchases.Rejected, nil},
		{purchases.Submitted, purchases.Approve, "creator", purchases.Submitted, purchases.ErrSelfDecision},
		{purchases.Draft, purchases.Approve, "approver", purchases.Draft, state.ErrTransition},
		{purchases.Approved, purchases.Approve, "approver", purchases.Approved, state.ErrTransition},
		{purchases.Rejected, purchases.Submit, "creator", purchases.Rejected, state.ErrTransition},
	} {
		next, err := machine.Next(test.from, test.event, purchases.Facts{
			ActorID: test.actor, CreatorID: "creator", AmountCents: 125_000,
		})
		if next != test.want || !errors.Is(err, test.err) {
			t.Fatalf("%s/%s: next=%s err=%v", test.from, test.event, next, err)
		}
	}
}
