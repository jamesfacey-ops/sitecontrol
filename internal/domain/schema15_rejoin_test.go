// schema15_rejoin_test.go — the SchemaVersion 15 shape itself (R-20 + R-23,
// the rejoin path): a visit row carries the key of the envelope that minted
// it; its raw columns are immutable and its rows undeletable BY TRIGGER;
// is_open is the ONE derived column and visit_open.go is the ONE file that
// writes it; and R-23's classification rule, as a truth table.
package domain

import (
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// R-23's rule, stated once in Visit.RestoredFromPeer, asserted case by case:
// OUR tag AND node_epoch BELOW the running config epoch — nothing else.
func TestR23RestoredFromPeerIsOurOwnTagBelowTheRunningEpochAndNothingElse(t *testing.T) {
	cases := []struct {
		name  string
		v     Visit
		site  string
		epoch int64
		want  bool
	}{
		{"our tag, one epoch below the running one — the peer's copy of our history", Visit{SiteID: "site-b", NodeEpoch: 1}, "site-b", 2, true},
		{"our tag, two epochs below (rebuilt twice)", Visit{SiteID: "site-b", NodeEpoch: 1}, "site-b", 3, true},
		{"our tag, the running epoch — observed at this gate", Visit{SiteID: "site-b", NodeEpoch: 2}, "site-b", 2, false},
		{"our tag, above the running epoch — the §8.4 bound makes this row impossible; never restored", Visit{SiteID: "site-b", NodeEpoch: 3}, "site-b", 2, false},
		{"another site's row — second-hand by nature, never this classification", Visit{SiteID: "site-a", NodeEpoch: 1}, "site-b", 2, false},
		{"a post never rebuilt: its epoch-1 rows are its own", Visit{SiteID: "site-b", NodeEpoch: 1}, "site-b", 1, false},
	}
	for _, c := range cases {
		if got := c.v.RestoredFromPeer(c.site, c.epoch); got != c.want {
			t.Errorf("%s: Visit{%s, epoch %d}.RestoredFromPeer(%s, %d) = %v, want %v", c.name, c.v.SiteID, c.v.NodeEpoch, c.site, c.epoch, got, c.want)
		}
	}
}

// The key on the row is the MINTING ENVELOPE's — for both mints: an arrival
// (GateCheckIn's trip_arrival) and an entry-on-completion
// (orientation_completed carrying a visit).
func TestSchema15AVisitCarriesTheKeyOfTheEnvelopeThatMintedIt(t *testing.T) {
	a := newDomainTestStore(t)
	p, err := CreatePerson(a, "site-a", 1, "Pat Keyed", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	num := "T12"
	veh, err := CreateVehicle(a, "site-a", 1, &num, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	b := newDomainTestStore(t)
	r20Replay(t, b, a, "site-a", 1)
	if _, _, err := GateCheckIn(b, "site-b", 2, GateCheckInInput{
		VehicleID: veh.VehicleID, Driver: PersonSlot{ExistingID: p.PersonID},
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	wm, err := b.Watermark("site-b", 2)
	if err != nil {
		t.Fatal(err)
	}
	open := r20OpenFor(t, b, "site-b", p.PersonID)
	if len(open) != 1 {
		t.Fatalf("open visits = %d, want 1", len(open))
	}
	// GateCheckIn authors dispatch then arrival: the arrival is the stream
	// head, and the arrival is what minted the visit.
	if open[0].NodeEpoch != 2 || open[0].Seq != wm {
		t.Errorf("arrival-minted visit carries key (epoch %d, seq %d), want the trip_arrival's (2, %d)", open[0].NodeEpoch, open[0].Seq, wm)
	}

	// Entry-on-completion: the completion envelope's key, on the wire shape
	// the rejoin path delivers (a prior-epoch record under the site's tag).
	c := newDomainTestStore(t)
	env := store.Envelope{SiteID: "site-b", NodeEpoch: 1, Seq: 1, SchemaVersion: store.SchemaVersion, OriginAuth: "site-b",
		Payload: mustJSON(t, OrientationCompleted{Type: evtOrientationCompleted, PersonID: "p-k",
			Visit: &ArrivalVisit{VisitID: "site-b-visit-keyed000000001", PersonID: "p-k"}})}
	if applied, err := c.Apply(env); err != nil || !applied {
		t.Fatalf("apply: %v %v", applied, err)
	}
	v, err := VisitByID(c, "site-b-visit-keyed000000001")
	if err != nil || v == nil {
		t.Fatalf("VisitByID: %v %v", v, err)
	}
	if v.NodeEpoch != 1 || v.Seq != 1 || v.IsOpen != 1 {
		t.Errorf("completion-minted visit = (epoch %d, seq %d, open %d), want (1, 1, 1)", v.NodeEpoch, v.Seq, v.IsOpen)
	}
	if !v.RestoredFromPeer("site-b", 2) {
		t.Errorf("that row, read by the re-imaged post (running epoch 2), must classify RESTORED")
	}
}

// visit is RAW + one derived column, BY TRIGGER: every column but is_open is
// immutable after INSERT and no row is ever deleted. A regression to a
// predicate fold on any other column aborts at the SQL layer.
func TestSchema15VisitRawColumnsAreImmutableAndRowsUndeletableByTrigger(t *testing.T) {
	a := newDomainTestStore(t)
	p, err := CreatePerson(a, "site-a", 1, "Pat Frozen", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	num := "T12"
	veh, err := CreateVehicle(a, "site-a", 1, &num, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := GateCheckIn(a, "site-a", 1, GateCheckInInput{
		VehicleID: veh.VehicleID, Driver: PersonSlot{ExistingID: p.PersonID},
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// LocalTx is a plain transaction on the same file (store/local.go) —
	// the trigger fires on whichever path issues the statement.
	run := func(q string) error {
		return a.LocalTx(func(tx *sql.Tx) error {
			_, err := tx.Exec(q)
			return err
		})
	}
	for _, q := range []string{
		`UPDATE visit SET person_snapshot_name = 'rewritten'`,
		`UPDATE visit SET node_epoch = 99`,
		`UPDATE visit SET seq = 99`,
		`UPDATE visit SET site_id = 'site-z'`,
		`UPDATE visit SET person_id = 'somebody-else'`,
		`UPDATE visit SET trip_id = NULL`,
		`UPDATE visit SET predates_orientation_rule = 1`,
	} {
		err := run(q)
		if err == nil || !strings.Contains(err.Error(), "immutable") {
			t.Errorf("%s: want the visit_raw_immutable trigger to abort, got %v", q, err)
		}
	}
	if err := run(`DELETE FROM visit`); err == nil || !strings.Contains(err.Error(), "never deleted") {
		t.Errorf("DELETE FROM visit: want the visit_no_delete trigger to abort, got %v", err)
	}
	// CONTROL: the ONE derived column is writable — the derivation's own
	// statements must not be what the trigger refuses.
	if err := run(`UPDATE visit SET is_open = is_open`); err != nil {
		t.Fatalf("CONTROL FAILED: an UPDATE touching only is_open was refused (%v) — the trigger is wider than the rule and visit_open.go's derivation could not run", err)
	}
	if n := len(r20OpenFor(t, a, "site-a", p.PersonID)); n != 1 {
		t.Fatalf("after the refused statements the visit must be untouched: open=%d, want 1", n)
	}
}

// The ONE place that writes visit.is_open is visit_open.go's derivation — a
// census over production Go under internal/, so a predicate UPDATE creeping
// back into a fold is named by file. Comment lines are skipped (the folds
// keep a ⟦was⟧ record of the statements they no longer run); the census
// fails its own premise if visit_open.go stops carrying its two statements.
func TestSchema15TheOnlyUpdateOfTheVisitTableIsTheDerivationInVisitOpenGo(t *testing.T) {
	root := filepath.Join("..", "..", "internal")
	re := regexp.MustCompile(`\bUPDATE\s+visit\b`)
	hits := map[string][]int{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(strings.TrimPrefix(path, root+string(filepath.Separator)))
		for i, line := range strings.Split(string(raw), "\n") {
			tl := strings.TrimSpace(line)
			if strings.HasPrefix(tl, "//") || strings.HasPrefix(tl, "--") {
				continue
			}
			if re.MatchString(line) {
				hits[rel] = append(hits[rel], i+1)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	const only = "domain/visit_open.go"
	if got := len(hits[only]); got != 2 {
		t.Fatalf("PREMISE: %s carries %d `UPDATE visit` statements (lines %v), want exactly 2 — the derivation's close-superseded and set-top statements", only, got, hits[only])
	}
	for file, lines := range hits {
		if file != only {
			t.Errorf("%s lines %v: an `UPDATE visit` outside the derivation — is_open is DERIVED (SchemaVersion 15, R-20); a fold that mutates presence by predicate is order-dependent across a site's own epochs. Route the rule through visit_open.go", file, lines)
		}
	}
}
