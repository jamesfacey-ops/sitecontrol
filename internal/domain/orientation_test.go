// orientation_test.go — SCHEMA-11 step 4 witnesses for orientation.go
// (D-88, D-95, D-91; reports/witness/STEP4-ORIENTATION.md §6). Every
// refusal and property here was produced RED first — on a deliberate
// one-line mutant of orientation.go — and the commands are recorded in the
// report's §7.
package domain

import (
	"bytes"
	"database/sql"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

const (
	orientTestSite  = "site-b" // the pad — the orienting site in every fixture below
	orientTestOther = "site-a" // the ice road
	orientTestPost  = "Pad Site Control"
)

// orientWire builds the wire payload for either event, keyed by the FROZEN
// literals so a builder that spells one differently reds every fixture.
func orientWire(typ, personID string, recordedBy *string) map[string]any {
	m := map[string]any{"type": typ, "person_id": personID}
	if recordedBy != nil {
		m["recorded_by"] = *recordedBy
	}
	return m
}

func orientDump(t *testing.T, st *store.Store, name string) []byte {
	t.Helper()
	b, err := st.DumpProjection(name)
	if err != nil {
		t.Fatalf("dump %s: %v", name, err)
	}
	return b
}

func orientHeld(t *testing.T, st *store.Store, personID, siteID string) *OrientationRow {
	t.Helper()
	row, err := OrientationHeld(st, personID, siteID)
	if err != nil {
		t.Fatalf("OrientationHeld(%s, %s): %v", personID, siteID, err)
	}
	return row
}

func orientWatermark(t *testing.T, st *store.Store, siteID string, epoch int64) int64 {
	t.Helper()
	wm, err := st.Watermark(siteID, epoch)
	if err != nil {
		t.Fatal(err)
	}
	return wm
}

// --- §6 row 1: the fold, both directions, and the raw tables' discipline ---

func TestOrientationCompletionFoldsRawAndDerivedRowsAndRetractionRemovesOnlyTheDerived(t *testing.T) {
	st := w9bNewStore(t)
	w9bSeedPersons(t, st, orientTestOther, 1, "diaz")

	env, err := CompleteOrientation(st, orientTestSite, 1, "diaz", w9bStr(orientTestPost), 0, "default")
	if err != nil {
		t.Fatalf("CompleteOrientation: %v", err)
	}
	row := orientHeld(t, st, "diaz", orientTestSite)
	if row == nil {
		t.Fatal("after a completion, OrientationHeld(diaz, site-b) = nil — the derived row was not written")
	}
	if row.NodeEpoch != env.NodeEpoch || row.Seq != env.Seq || row.RecordedBy == nil || *row.RecordedBy != orientTestPost {
		t.Fatalf("derived row = %+v, want the completing event's key %v and recorded_by %q", row, env.Key(), orientTestPost)
	}
	if other := orientHeld(t, st, "diaz", orientTestOther); other != nil {
		t.Fatalf("a site-b completion produced a site-a row: %+v — the site is the envelope's, and only the envelope's", other)
	}
	comps, err := dumpSection[OrientationCompletionRow](st, "orientation_completion")
	if err != nil || len(comps) != 1 || comps[0].PersonID != "diaz" || comps[0].SiteID != orientTestSite {
		t.Fatalf("orientation_completion = %+v (err %v), want exactly one site-b row for diaz", comps, err)
	}

	if _, err := RetractOrientation(st, orientTestSite, 1, "diaz", w9bStr(orientTestPost), 0, "default"); err != nil {
		t.Fatalf("RetractOrientation: %v", err)
	}
	if row := orientHeld(t, st, "diaz", orientTestSite); row != nil {
		t.Fatalf("after a retraction the derived row survives: %+v", row)
	}
	comps, _ = dumpSection[OrientationCompletionRow](st, "orientation_completion")
	rets, _ := dumpSection[OrientationRetractionRow](st, "orientation_retraction")
	if len(comps) != 1 || len(rets) != 1 {
		t.Fatalf("raw tables after retraction: %d completions, %d retractions — want 1 and 1: the raw record is never rewritten", len(comps), len(rets))
	}

	// The raw tables are INSERT-only BY TRIGGER, not by policy.
	for _, tbl := range []string{"orientation_completion", "orientation_retraction"} {
		for _, stmt := range []string{
			`UPDATE ` + tbl + ` SET person_id = 'x'`,
			`DELETE FROM ` + tbl,
		} {
			err := st.LocalTx(func(tx *sql.Tx) error { _, e := tx.Exec(stmt); return e })
			if err == nil || !strings.Contains(err.Error(), "INSERT-only") {
				t.Fatalf("%q succeeded or failed for the wrong reason (%v) — the trigger is absent", stmt, err)
			}
		}
	}
}

// --- §6 row 2: order-independence across epochs; dangling tolerance -------

// TestOrientationDerivationIsOrderIndependentAcrossEpochsAndDanglingTolerant
// is the reason the projection is raw + derived. A completion on epoch 1,
// its retraction on epoch 2 and a re-completion on epoch 2 are applied in
// every permutation; the three tables' dumps must be byte-identical and the
// answer must be "held" (the latest key is a completion). A retraction
// delivered before any completion leaves no row.
func TestOrientationDerivationIsOrderIndependentAcrossEpochsAndDanglingTolerant(t *testing.T) {
	by := w9bStr(orientTestPost)
	fixture := []store.Envelope{
		w9bEnvelope(orientTestSite, 1, 1, w9bBase, orientWire(evtOrientationCompleted, "diaz", by)),
		w9bEnvelope(orientTestSite, 2, 1, w9bBase+w9bMinute, orientWire(evtOrientationRetracted, "diaz", by)),
		w9bEnvelope(orientTestSite, 2, 2, w9bBase+2*w9bMinute, orientWire(evtOrientationCompleted, "diaz", by)),
	}
	// Streams are contiguous per (site, epoch), so within epoch 2 the
	// retraction (seq 1) always precedes the re-completion (seq 2); every
	// CROSS-stream interleaving of the two streams is listed.
	perms := [][]int{{0, 1, 2}, {1, 0, 2}, {1, 2, 0}}
	var base [3][]byte
	for pi, perm := range perms {
		st := w9bNewStore(t)
		for step, i := range perm {
			w9bApply(t, st, fixture[i])
			// Dangling tolerance: after ONLY the retraction, no row.
			if step == 0 && i == 1 {
				if row := orientHeld(t, st, "diaz", orientTestSite); row != nil {
					t.Fatalf("perm %v: a retraction delivered before any completion produced a row: %+v", perm, row)
				}
			}
		}
		row := orientHeld(t, st, "diaz", orientTestSite)
		if row == nil || row.NodeEpoch != 2 || row.Seq != 2 {
			t.Fatalf("perm %v: final derived row = %+v, want the epoch-2 seq-2 completion (the latest key)", perm, row)
		}
		dumps := [3][]byte{
			orientDump(t, st, "orientation"),
			orientDump(t, st, "orientation_completion"),
			orientDump(t, st, "orientation_retraction"),
		}
		if pi == 0 {
			base = dumps
			continue
		}
		for k, name := range []string{"orientation", "orientation_completion", "orientation_retraction"} {
			if !bytes.Equal(dumps[k], base[k]) {
				t.Fatalf("perm %v: %s dump differs from perm %v — the derivation is order-dependent\n got: %s\nwant: %s", perm, name, perms[0], dumps[k], base[k])
			}
		}
	}

	// The two-event case, both orders: complete@1 then retract@2 vs retract@2
	// then complete@1 — NOT held either way.
	for _, order := range [][]int{{0, 1}, {1, 0}} {
		st := w9bNewStore(t)
		for _, i := range order {
			w9bApply(t, st, fixture[i])
		}
		if row := orientHeld(t, st, "diaz", orientTestSite); row != nil {
			t.Fatalf("order %v: complete@epoch1 + retract@epoch2 left a row: %+v — the retraction carries the greater key", order, row)
		}
	}
}

// --- §6 rows 3, 4: the completion edge's refusals, nothing authored --------

func TestOrientationDuplicateCompletionIsRefusedPreAppendWithItsSentence(t *testing.T) {
	st := w9bNewStore(t)
	w9bSeedPersons(t, st, orientTestOther, 1, "diaz", "evans", "clark")
	if _, err := CompleteOrientation(st, orientTestSite, 1, "diaz", w9bStr(orientTestPost), 0, "default"); err != nil {
		t.Fatalf("first completion: %v", err)
	}
	before := orientWatermark(t, st, orientTestSite, 1)

	_, err := CompleteOrientation(st, orientTestSite, 1, "diaz", w9bStr(orientTestPost), 0, "default")
	var ref *OrientationRefusal
	if !errors.As(err, &ref) || ref.Code != OrientationAlreadyHeld {
		t.Fatalf("second completion: err = %v (%T), want *OrientationRefusal{AlreadyHeld}", err, err)
	}
	const want = `domain: orientation: diaz already holds site-b's orientation (recorded by Pad Site Control); nothing was recorded`
	if got := ref.Error(); got != want {
		t.Fatalf("refusal sentence = %q, want %q", got, want)
	}
	if after := orientWatermark(t, st, orientTestSite, 1); after != before {
		t.Fatalf("the refused completion AUTHORED: watermark %d → %d", before, after)
	}

	// The bulk edge: validate ALL, then author — a refusal anywhere means
	// nothing landed for anyone in the submission.
	_, err = CompleteOrientations(st, orientTestSite, 1, []string{"evans", "diaz"}, w9bStr(orientTestPost), 0, "default")
	if !errors.As(err, &ref) || ref.Code != OrientationAlreadyHeld || ref.PersonID != "diaz" {
		t.Fatalf("bulk with a held id: err = %v, want AlreadyHeld naming diaz", err)
	}
	if after := orientWatermark(t, st, orientTestSite, 1); after != before {
		t.Fatalf("the refused bulk submission authored evans before refusing diaz: watermark %d → %d — validate-all-then-author is broken", before, after)
	}
	if orientHeld(t, st, "evans", orientTestSite) != nil {
		t.Fatal("evans was entered by a refused submission")
	}
	// A duplicate id WITHIN one submission is the same mistake.
	_, err = CompleteOrientations(st, orientTestSite, 1, []string{"evans", "evans"}, w9bStr(orientTestPost), 0, "default")
	if !errors.As(err, &ref) || ref.Code != OrientationAlreadyHeld || ref.HeldBy != "this submission" {
		t.Fatalf("bulk with a repeated id: err = %v, want AlreadyHeld held by 'this submission'", err)
	}
	if after := orientWatermark(t, st, orientTestSite, 1); after != before {
		t.Fatalf("the repeated-id submission authored: watermark %d → %d", before, after)
	}
	// And the clean submission records one event per person, in order.
	envs, err := CompleteOrientations(st, orientTestSite, 1, []string{"evans", "clark"}, w9bStr(orientTestPost), 0, "default")
	if err != nil || len(envs) != 2 || envs[0].Seq != before+1 || envs[1].Seq != before+2 {
		t.Fatalf("clean bulk: envs = %v, err = %v; want two consecutive events", envs, err)
	}
	for _, id := range []string{"evans", "clark"} {
		if orientHeld(t, st, id, orientTestSite) == nil {
			t.Fatalf("%s not held after the clean bulk submission", id)
		}
	}
}

func TestOrientationUnknownOrUnnamedPersonIsRefusedAndNothingAuthored(t *testing.T) {
	st := w9bNewStore(t)
	before := orientWatermark(t, st, orientTestSite, 1)
	cases := []struct {
		id   string
		code OrientationRefusalCode
		want string
	}{
		{"", OrientationPersonRequired, `domain: orientation: no person was named; nothing was recorded`},
		{"nobody", OrientationPersonUnknown, `domain: orientation: person nobody is not known at this site; nothing was recorded`},
	}
	for _, tc := range cases {
		for name, call := range map[string]func() error{
			"complete": func() error {
				_, err := CompleteOrientation(st, orientTestSite, 1, tc.id, nil, 0, "default")
				return err
			},
			"retract": func() error {
				_, err := RetractOrientation(st, orientTestSite, 1, tc.id, nil, 0, "default")
				return err
			},
		} {
			err := call()
			var ref *OrientationRefusal
			if !errors.As(err, &ref) || ref.Code != tc.code {
				t.Fatalf("%s(%q): err = %v, want code %v", name, tc.id, err, tc.code)
			}
			if got := ref.Error(); got != tc.want {
				t.Fatalf("%s(%q): sentence = %q, want %q", name, tc.id, got, tc.want)
			}
		}
	}
	if after := orientWatermark(t, st, orientTestSite, 1); after != before {
		t.Fatalf("a refused edge authored: watermark %d → %d", before, after)
	}
}

// --- §6 rows 5, 6, 7: retraction is own-site by construction ---------------

func TestOrientationRetractionIsOwnSiteByConstructionAndReCompletionIsAccepted(t *testing.T) {
	st := w9bNewStore(t)
	w9bSeedPersons(t, st, orientTestOther, 1, "diaz")
	if _, err := CompleteOrientation(st, orientTestSite, 1, "diaz", w9bStr(orientTestPost), 0, "default"); err != nil {
		t.Fatal(err)
	}

	// The EDGE at site-a: diaz holds site-b's orientation and not site-a's,
	// so site-a has nothing to retract — and site-b's row is not its to touch.
	beforeA := orientWatermark(t, st, orientTestOther, 1)
	_, err := RetractOrientation(st, orientTestOther, 1, "diaz", w9bStr("Ice Road"), 0, "default")
	var ref *OrientationRefusal
	if !errors.As(err, &ref) || ref.Code != OrientationNotHeld {
		t.Fatalf("site-a retracting a site-b orientation: err = %v, want NotHeld", err)
	}
	const want = `domain: orientation: diaz does not hold site-a's orientation; there is nothing to retract`
	if got := ref.Error(); got != want {
		t.Fatalf("sentence = %q, want %q", got, want)
	}
	if orientWatermark(t, st, orientTestOther, 1) != beforeA {
		t.Fatal("the refused cross-site retraction authored")
	}
	if orientHeld(t, st, "diaz", orientTestSite) == nil {
		t.Fatal("site-b's row was disturbed by site-a's refused retraction")
	}

	// The WIRE at site-a: a peer's retraction folds (never refuses) and acts
	// only on site-a's OWN (diaz, site-a) row — which does not exist.
	w9bApplyNext(t, st, orientTestOther, 1, w9bBase+w9bMinute, orientWire(evtOrientationRetracted, "diaz", w9bStr("Ice Road")))
	if orientHeld(t, st, "diaz", orientTestSite) == nil {
		t.Fatal("a site-a wire retraction removed site-b's row — the derivation keyed on the payload, not the envelope's site")
	}
	if orientHeld(t, st, "diaz", orientTestOther) != nil {
		t.Fatal("a site-a wire retraction produced a site-a row")
	}

	// D-95 at the recording site: accepted; then re-completion is accepted —
	// "forever" governs the person, not the record.
	if _, err := RetractOrientation(st, orientTestSite, 1, "diaz", w9bStr(orientTestPost), 0, "default"); err != nil {
		t.Fatalf("own-site retraction: %v", err)
	}
	if orientHeld(t, st, "diaz", orientTestSite) != nil {
		t.Fatal("own-site retraction left the row")
	}
	if _, err := RetractOrientation(st, orientTestSite, 1, "diaz", w9bStr(orientTestPost), 0, "default"); !errors.As(err, &ref) || ref.Code != OrientationNotHeld {
		t.Fatalf("second retraction: err = %v, want NotHeld", err)
	}
	if _, err := CompleteOrientation(st, orientTestSite, 1, "diaz", w9bStr(orientTestPost), 0, "default"); err != nil {
		t.Fatalf("re-completion after a retraction: %v — a retracted mis-tick must be completable again", err)
	}
	if orientHeld(t, st, "diaz", orientTestSite) == nil {
		t.Fatal("re-completion did not restore the row")
	}
	// Read-everything (D-87): every site sees the rows, by person and by site.
	ofDiaz, err := OrientationsOf(st, "diaz")
	if err != nil || len(ofDiaz) != 1 || ofDiaz[0].SiteID != orientTestSite {
		t.Fatalf("OrientationsOf(diaz) = %+v (%v), want exactly the site-b row", ofDiaz, err)
	}
	atB, err := OrientationsAt(st, orientTestSite)
	if err != nil || len(atB) != 1 || atB[0].PersonID != "diaz" {
		t.Fatalf("OrientationsAt(site-b) = %+v (%v), want exactly diaz", atB, err)
	}
}

// --- §6 row 8: carrier coverage and the B-2 key census ---------------------

// orientationExemptReason is the WRITTEN reason `orientation_retracted` is a
// non-carrier — never silently absent (VF §7.7). Cancellation-shaped: a
// targeted withdrawal of ONE recorded membership assertion, no value slot,
// nothing for Outcome to resolve — the same ground as arrival_ref_retracted
// and alert_ack_retracted. `orientation_completed` ends in no carrier-class
// suffix at all and is asserted so below.
const orientationExemptReason = "orientation_retracted: cancellation-shaped withdrawal of one (person, site) membership assertion — no value slot, nothing for Outcome to resolve (VF §7.7; F6's algebra)"

func TestOrientationRetractedIsEnumeratedByTheWidenedSuffixAndExemptWithAWrittenReason(t *testing.T) {
	if orientationExemptReason == "" {
		t.Fatal("the exemption has no written reason")
	}
	st := w9bNewStore(t)
	w9bSeedPersons(t, st, orientTestOther, 1, "diaz")
	c, err := CompleteOrientation(st, orientTestSite, 1, "diaz", w9bStr(orientTestPost), 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	r, err := RetractOrientation(st, orientTestSite, 1, "diaz", nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	streams := [][2]any{{orientTestOther, int64(1)}, {orientTestSite, int64(1)}}

	// The widened classification reaches the retraction; the delivered one
	// does not — and NEITHER reaches the completion, which is therefore not
	// a carrier-class type and needs no exemption row.
	narrow := w9bInventory(t, st, w9bDeliveredClassSuffixes, streams)
	wide := w9bInventory(t, st, w9bCarrierClassSuffixes, streams)
	if w9bContains(narrow, evtOrientationRetracted) {
		t.Fatalf("the delivered `_amended`/`_corrected` classification reaches %q: %v", evtOrientationRetracted, narrow)
	}
	if !w9bContains(wide, evtOrientationRetracted) {
		t.Fatalf("the widened classification does not enumerate %q: %v — the exemption would be a dead row", evtOrientationRetracted, wide)
	}
	if w9bContains(wide, evtOrientationCompleted) || w9bContains(narrow, evtOrientationCompleted) {
		t.Fatalf("%q is enumerated as a carrier-class type; it is membership-shaped and carries no value slot", evtOrientationCompleted)
	}
	// CONTROL: with no exemption row the coverage judgement flags it.
	if got := w4CoverageProblems(wide, nil, IsRegisteredCarrier); len(got) != 1 || !strings.Contains(got[0], evtOrientationRetracted) {
		t.Fatalf("CONTROL FAILED: the coverage judgement did not flag the unexempted %q (got %v)", evtOrientationRetracted, got)
	}
	// The clean pass: exempt with the written reason, no problems. (The
	// shared LOCKED `w4ExemptNonCarriers` is judged by W9b-4 over ITS log,
	// which holds no orientation event; this list is this step's.)
	if got := w4CoverageProblems(wide, []string{evtOrientationRetracted}, IsRegisteredCarrier); len(got) != 0 {
		t.Fatalf("coverage problems on the live log: %v", got)
	}
	if IsRegisteredCarrier(evtOrientationCompleted) || IsRegisteredCarrier(evtOrientationRetracted) {
		t.Fatal("an orientation event is registered as a §4.4 carrier — it has no fact vocabulary")
	}

	// B-2: the wire key sets, read from the LOG.
	for _, tc := range []struct {
		k    store.Key
		want []string
	}{
		{c.Key(), []string{"person_id", "recorded_by", "type"}},
		{r.Key(), []string{"person_id", "type"}}, // recorded_by omitted when nil
	} {
		if got := w9bLogPayloadKeys(t, st, tc.k); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("payload keys at %v = %v, want %v", tc.k, got, tc.want)
		}
	}
	// And no payload names a site.
	for _, k := range []store.Key{c.Key(), r.Key()} {
		for _, key := range w9bLogPayloadKeys(t, st, k) {
			if strings.Contains(key, "site") {
				t.Fatalf("payload at %v carries a site-shaped key %q — the site is the envelope's, never the payload's", k, key)
			}
		}
	}
}

// --- structural: STRICT, no status-shaped column, triggers present ---------

func TestOrientationTablesAreStrictCarryNoStatusShapedColumnAndTheRawOnesHaveTriggers(t *testing.T) {
	st := w9bNewStore(t)
	statusShaped := map[string]bool{"status": true, "superseded": true, "effective": true, "is_superseded": true, "is_effective": true, "stale": true, "current": true, "winner": true}
	wantCols := map[string][]string{
		// SchemaVersion 12 (R-16): entry-on-completion adds arrival_ref/
		// trip_ref, mirroring OrientationCompleted's own new payload keys —
		// neither is status-shaped (they name a fixed arrival, never a
		// computed state), so they do not trip the map above.
		"orientation_completion": {"site_id", "node_epoch", "seq", "person_id", "recorded_by", "arrival_ref", "trip_ref"},
		"orientation_retraction": {"site_id", "node_epoch", "seq", "person_id", "recorded_by"},
		"orientation":            {"person_id", "site_id", "node_epoch", "seq", "recorded_by"},
	}
	wantTriggers := map[string]int{"orientation_completion": 2, "orientation_retraction": 2, "orientation": 0}
	names := make([]string, 0, len(wantCols))
	for n := range wantCols {
		names = append(names, n)
	}
	sort.Strings(names)
	err := st.LocalTx(func(tx *sql.Tx) error {
		for _, tbl := range names {
			var ddl string
			if err := tx.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&ddl); err != nil {
				t.Fatalf("%s: %v", tbl, err)
			}
			if !strings.Contains(strings.ToUpper(ddl), "STRICT") {
				t.Fatalf("%s is not STRICT: %s", tbl, ddl)
			}
			rows, err := tx.Query(`SELECT name FROM pragma_table_info(?) ORDER BY cid`, tbl)
			if err != nil {
				return err
			}
			var cols []string
			for rows.Next() {
				var c string
				if err := rows.Scan(&c); err != nil {
					rows.Close()
					return err
				}
				if statusShaped[strings.ToLower(c)] {
					rows.Close()
					t.Fatalf("%s carries a status-shaped column %q — supersession is computed, never stored", tbl, c)
				}
				cols = append(cols, c)
			}
			rows.Close()
			if !reflect.DeepEqual(cols, wantCols[tbl]) {
				t.Fatalf("%s columns = %v, want exactly %v", tbl, cols, wantCols[tbl])
			}
			var n int
			if err := tx.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='trigger' AND tbl_name=?`, tbl).Scan(&n); err != nil {
				return err
			}
			if n != wantTriggers[tbl] {
				t.Fatalf("%s has %d triggers, want %d", tbl, n, wantTriggers[tbl])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
