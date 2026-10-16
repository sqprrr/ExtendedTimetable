package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

func TestSnapshot(t *testing.T) {
	st := openTest(t)
	bg := context.Background()
	newGroup := func(ctx context.Context, code string) {
		t.Helper()
		if err := st.CreateGroup(ctx, &store.Group{Code: code, Name: code, CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	count := func(ctx context.Context) int {
		t.Helper()
		gs, err := st.ListGroups(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return len(gs)
	}
	newGroup(bg, "A")

	ctx, end := st.WithSnapshot(bg)
	defer end()
	if n := count(ctx); n != 1 {
		t.Fatalf("groups in the snapshot: %d", n)
	}
	before := st.Version(ctx)

	// Another request's write is not seen: the snapshot holds.
	newGroup(bg, "B")
	if n := count(ctx); n != 1 {
		t.Errorf("a snapshot must not see later writes of others: %d groups", n)
	}
	if st.Version(ctx) != before {
		t.Error("the snapshot's version must be the one it began at")
	}
	if st.Version(bg) == before {
		t.Error("a write must change the version")
	}

	// The request's own write ends the snapshot, so it reads what it wrote.
	newGroup(ctx, "C")
	if n := count(ctx); n != 3 {
		t.Errorf("after its own write, a request must see it: %d groups", n)
	}
	if err := st.InTx(ctx, func(q *store.Queries) error {
		return q.CreateGroup(ctx, &store.Group{Code: "D", Name: "D", CreatedAt: time.Now()})
	}); err != nil {
		t.Fatal(err)
	}
	if n := count(ctx); n != 4 {
		t.Errorf("after its own transaction, a request must see it: %d groups", n)
	}

	// Once ended, the context's reads run on their own.
	end()
	newGroup(bg, "E")
	if n := count(ctx); n != 5 {
		t.Errorf("after the end, reads must see the database as it is: %d groups", n)
	}
}
