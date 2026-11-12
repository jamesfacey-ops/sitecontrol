// r23_restored_marker_test.go — R-23 (James, 2026-09-13: "Restore them, but
// mark them as restored"), at the surfaces: the muster sheet (live and
// printed) and the gate roster.
//
// THE MEASURED HAZARD (reports/witness/CLEAN-NEGATIVES-RECHECK.md, plant
// C2b, docs d9a59ae): a peer serving forged self-history — a record under
// OUR site tag at a PRIOR epoch, which repl/engine.go's §8.4 exception
// admits from any authenticated peer — put a stranger's OPEN VISIT under
// our tag. Only the epoch was bounded; the content was not, and cannot be:
// a wiped post cannot tell genuine recovered history from forged, because
// the wipe destroyed the evidence it would need. The peer's copy is also
// our ONLY backup, so refusing it means a post wiped mid-shift with twenty
// people inside re-enters all twenty by hand.
//
// James's ruling keeps the data and stops it masquerading: every visit
// under our tag that THIS incarnation of the post never observed —
// node_epoch below the running config epoch, the same bound the §8.4 arm
// enforces — is listed, counted, and TAGGED RESTORED, per name and in a
// count, on the muster and the roster. Second-hand presence is never
// presented as first-hand; the muster never claims more than it knows.
//
// This test applies the C2b payload byte-for-byte through store.Apply —
// the identical call repl's pullStream makes for a self-history page — and
// reads the rendered pages. RED on the unchanged tree (the stranger is
// listed with no marking at all); GREEN with the marker.
package web

import (
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

func TestR23AStrangerRestoredThroughSelfHistoryIsListedAndMarkedOnMusterAndRoster(t *testing.T) {
	st := w3OpenRegistered(t, "r23-restored.db")
	cfg := w3ConfigSiteB(st.Path)
	cfg.NodeEpoch = 2 // the re-imaged post: everything it observes is authored on epoch 2
	srv := New(cfg, st)

	// C2b: the peer's "copy of our history" — a completion for a person
	// site-b never enrolled, carrying a visit, under our tag at epoch 1.
	const forgedVisit = "site-b-visit-forged000000001"
	strangerName := "Diaz, Forged"
	forged := w9aEnvelope(t, "site-b", 1, 1, w2Base, map[string]any{
		"type": "orientation_completed", "person_id": "p-stranger",
		"visit": map[string]any{"visit_id": forgedVisit, "person_id": "p-stranger", "person_name": strangerName},
	})
	if applied, err := st.Apply(forged); err != nil || !applied {
		t.Fatalf("the self-history page did not apply (applied=%v err=%v) — the fold never refuses (W3-2), so this fixture no longer models C2b", applied, err)
	}

	// CONTROL: a first-hand check-in on the running epoch — observed at
	// this gate by this post. It must NOT be tagged.
	w9aSeedPerson(t, st, "site-b", 2, "p-local", "Local, Lee")
	num := "TRK-118"
	veh, err := domain.CreateVehicle(st, "site-b", 2, &num, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := domain.GateCheckIn(st, "site-b", 2, domain.GateCheckInInput{
		VehicleID: veh.VehicleID, Driver: domain.PersonSlot{ExistingID: "p-local"},
	}, 0, "default"); err != nil {
		t.Fatalf("local check-in: %v", err)
	}
	open, err := domain.OpenVisitsAt(st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	var localVisit string
	for _, v := range open {
		if v.PersonID == "p-local" {
			localVisit = v.VisitID
		}
	}
	if len(open) != 2 || localVisit == "" {
		t.Fatalf("fixture: open visits at site-b = %d (local=%q), want 2 — the stranger's restored visit AND the local one; recovery must still WORK", len(open), localVisit)
	}

	for _, route := range []string{"/muster", "/muster/print"} {
		page := rawPage(t, srv, route)
		// "Restore them": the stranger IS on the sheet and IS in the total.
		if !strings.Contains(page, `data-tally-count="on-site">2<`) {
			t.Errorf("%s: the on-site total must count the restored person too (want 2); recovery is not to be refused — page tally: %s", route, tallySnippet(page))
		}
		// "…but mark them as restored": per name, and in a count.
		if !strings.Contains(page, `data-restored-visit="`+forgedVisit+`"`) {
			t.Errorf("%s: the stranger's visit %s is listed WITHOUT the RESTORED tag — second-hand presence presented as first-hand (R-23)", route, forgedVisit)
		}
		if !strings.Contains(page, `data-restored-count="1"`) {
			t.Errorf("%s: the sheet does not say how many names are restored (want a count of 1)", route)
		}
		if strings.Contains(page, `data-restored-visit="`+localVisit+`"`) {
			t.Errorf("%s: the LOCAL first-hand visit %s is tagged RESTORED — the classification must be own-tag AND below-the-running-epoch, not everything", route, localVisit)
		}
		if strings.Contains(page, "unsynced") {
			t.Errorf("%s: the R-23 wording must not borrow the clock-verdict vocabulary (sentinel R28)", route)
		}
	}
	// The printed sheet names people by their frozen snapshot — the
	// stranger's forged name must appear there, tagged, not dropped.
	if page := rawPage(t, srv, "/muster/print"); !strings.Contains(page, strangerName) {
		t.Errorf("/muster/print: the restored person's name %q is missing from the printed sheet — restore them", strangerName)
	}

	// The gate roster: the officer's working list says the same thing.
	gate := rawPage(t, srv, "/gate")
	if !strings.Contains(gate, `data-restored-visit="`+forgedVisit+`"`) {
		t.Errorf("/gate: the roster lists the restored visit %s without the RESTORED tag", forgedVisit)
	}
	if !strings.Contains(gate, `data-roster-restored="1"`) {
		t.Errorf("/gate: the roster does not say how many rows are restored (want 1)")
	}
	if strings.Contains(gate, `data-restored-visit="`+localVisit+`"`) {
		t.Errorf("/gate: the LOCAL first-hand visit %s is tagged RESTORED", localVisit)
	}
}

// A post that was NOT re-imaged (running epoch 1) classifies nothing as
// restored: every row under its tag is at its own epoch. The rule is
// "below the running epoch", never "epoch 1".
func TestR23APostThatWasNeverRebuiltMarksNothingRestored(t *testing.T) {
	st := w3OpenRegistered(t, "r23-not-rebuilt.db")
	cfg := w3ConfigSiteB(st.Path) // NodeEpoch 1
	srv := New(cfg, st)
	w9aSeedPerson(t, st, "site-b", 1, "p-local", "Local, Lee")
	num := "TRK-118"
	veh, err := domain.CreateVehicle(st, "site-b", 1, &num, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := domain.GateCheckIn(st, "site-b", 1, domain.GateCheckInInput{
		VehicleID: veh.VehicleID, Driver: domain.PersonSlot{ExistingID: "p-local"},
	}, 0, "default"); err != nil {
		t.Fatalf("local check-in: %v", err)
	}
	for _, route := range []string{"/muster", "/muster/print", "/gate"} {
		page := rawPage(t, srv, route)
		if strings.Contains(page, `data-restored-visit=`) || strings.Contains(page, `data-restored-count=`) || strings.Contains(page, `data-roster-restored=`) {
			t.Errorf("%s: a post running on its first epoch tagged something RESTORED — nothing it holds under its own tag is second-hand", route)
		}
	}
}

func tallySnippet(page string) string {
	i := strings.Index(page, `data-tally-count="on-site"`)
	if i < 0 {
		return "(no on-site tally on the page)"
	}
	end := i + 80
	if end > len(page) {
		end = len(page)
	}
	return page[i:end]
}
