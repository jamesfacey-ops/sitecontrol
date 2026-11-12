// r3_ack_kind_cluster_test.go — ruling R3, second half, at the ack door and
// across two posts (James, 2026-10-04): an officer at the destination who
// acknowledges an amber VERIFY BY RADIO row must not silence the origin's
// live red for the same truck (L08 T-116). The door stamps the kind of row
// it was pressed on; the origin keeps its red.
package web

import (
	"net/http"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

func r3CopyStream(t *testing.T, from, to *store.Store, site string) {
	t.Helper()
	envs, err := from.Events(site, 1, 1, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	for _, env := range envs {
		if _, err := to.Apply(env); err != nil {
			t.Fatalf("apply %v: %v", env.Key(), err)
		}
	}
}

func TestR3AnAmberAckAtTheDestinationDoesNotSilenceTheOriginsRed(t *testing.T) {
	srv, st, _, trip := newAlertingServer(t) // site-b, the destination: amber VERIFY BY RADIO

	// The origin, site-a, as a second post with its own database.
	stA, err := store.Open(filepath.Join(t.TempDir(), "origin.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stA.Close() })
	if err := domain.Register(stA); err != nil {
		t.Fatal(err)
	}
	if err := deadline.Register(stA); err != nil {
		t.Fatal(err)
	}
	r3CopyStream(t, st, stA, "site-a")
	dA, err := deadline.New(stA, "site-a", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	envs, err := stA.Events("site-a", 1, trip.Seq, 1)
	if err != nil || len(envs) != 1 {
		t.Fatalf("fetch trip envelope: %v", err)
	}
	dA.WallNow = func() time.Time { return time.UnixMilli(envs[0].AuthoredAt.WallMS).Add(3 * time.Minute) }

	redAtA := func() (deadline.Alert, int) {
		b, err := dA.Evaluate()
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range b.Alerts {
			if a.TripID == trip.TripID {
				return a, len(b.Banner())
			}
		}
		t.Fatalf("trip %s is not on the origin's board", trip.TripID)
		return deadline.Alert{}, 0
	}
	if a, n := redAtA(); a.Status != deadline.StatusOverdueLive || n != 1 {
		t.Fatalf("precondition: the origin's row is %q with %d banner rows, want one live red", a.Status, n)
	}
	b, err := srv.Alerts.Evaluate()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range b.Alerts {
		if a.TripID == trip.TripID && a.Status != deadline.StatusOverdueUnresolved {
			t.Fatalf("precondition: the destination's row is %q, want amber (overdue_unresolved)", a.Status)
		}
	}

	if rec := postForm(t, srv, "/alerts/ack", url.Values{"trip_id": {trip.TripID}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("ack at the destination = %d %s", rec.Code, rec.Body.String())
	}
	r3CopyStream(t, st, stA, "site-b") // the ack replicates to the origin

	if a, n := redAtA(); a.Acked || n != 1 {
		t.Fatalf("R3: the destination's ack on its AMBER row silenced the origin's RED (Acked=%v, banner rows %d) — ack per alert kind", a.Acked, n)
	}
}
