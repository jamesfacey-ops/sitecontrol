// orientation_wire_test.go — tester-go's independent witnesses for SCHEMA-11
// step 4 (reports/witness/TESTER-step4.md). The builder's orientation_test.go
// STATES three properties; these PRODUCE them on the wire, each watched red
// on a one-line mutant of orientation.go before it was trusted green (the
// report's §2/§6 record the commands):
//
//  1. THE SITE IS THE ENVELOPE'S, NEVER THE PAYLOAD'S. A wire payload that
//     CLAIMS another site — under `site_id` or `site` — is inert at the fold:
//     the raw row lands under the envelope's site, the derived row is the
//     envelope's site's, and the claimed site's row is neither written nor
//     removed. This is D-87's ownership guarantee as the wire expresses it;
//     if it leaked, one site could mark another's orientation on or off.
//  2. THE CLOCK ORDERS NOTHING (D-90/D-92). Completion and retraction are
//     delivered in BOTH orders, as SEPARATE batches and INSIDE one batch,
//     with the wall clock DISAGREEING with the key (a re-imaged post whose
//     clock came up behind). The answer follows `store.Key`, and all three
//     dumps are byte-identical under every delivery shape.
//  3. A REFUSED EDGE LEAVES NO ENVELOPE. Not merely "the watermark stands
//     still": the stream's log is DeepEqual to what it was, refusal by
//     refusal.
package domain

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"sitecontrol/internal/store"
)

// orientApplyBatch delivers one batch through the same ApplyBatch the
// replication engine uses, and treats a refusal or a dedup as a finding
// (the fold never refuses — W3-2).
func orientApplyBatch(t *testing.T, st *store.Store, envs []store.Envelope) {
	t.Helper()
	res, err := st.ApplyBatch(envs)
	if err != nil {
		t.Fatalf("ApplyBatch(%d envelopes): the fold REFUSED: %v", len(envs), err)
	}
	if len(res) != len(envs) {
		t.Fatalf("ApplyBatch applied %d of %d", len(res), len(envs))
	}
	for i, ok := range res {
		if !ok {
			t.Fatalf("ApplyBatch: %v deduped unexpectedly", envs[i].Key())
		}
	}
}

// orientRawCount counts raw rows in one of the two INSERT-only tables under
// one site, straight from SQL, so a row under the wrong site cannot hide
// behind a decode.
func orientRawCount(t *testing.T, st *store.Store, table, siteID string) int {
	t.Helper()
	var n int
	if err := st.LocalTx(func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT count(*) FROM `+table+` WHERE site_id = ?`, siteID).Scan(&n)
	}); err != nil {
		t.Fatalf("count %s where site_id=%q: %v", table, siteID, err)
	}
	return n
}

// --- 1. the site comes from the envelope ------------------------------------

// TestOrientationSiteIsTheEnvelopesAPayloadClaimingAnotherSiteIsInertAtTheFold
// crafts envelopes AUTHORED BY site-a whose payloads claim site-b, under both
// spellings a leak could take, and shows each acts on site-a's own (person,
// site-a) row and on nothing of site-b's — raw and derived, completion and
// retraction. The claim keys are a fixed slice ((C1): forced order).
func TestOrientationSiteIsTheEnvelopesAPayloadClaimingAnotherSiteIsInertAtTheFold(t *testing.T) {
	by := w9bStr("Ice Road")
	for _, claimKey := range []string{"site_id", "site"} {
		t.Run(claimKey, func(t *testing.T) {
			st := w9bNewStore(t)

			// A completion authored by site-a whose payload claims site-b.
			p := orientWire(evtOrientationCompleted, "diaz", by)
			p[claimKey] = orientTestSite
			w9bApply(t, st, w9bEnvelope(orientTestOther, 1, 1, w9bBase, p))

			if row := orientHeld(t, st, "diaz", orientTestSite); row != nil {
				t.Fatalf("a site-a envelope whose payload said %s=%q produced a (diaz, site-b) DERIVED row %+v — the payload's site was honoured; one site marked another's orientation", claimKey, orientTestSite, row)
			}
			if got := orientRawCount(t, st, "orientation_completion", orientTestSite); got != 0 {
				t.Fatalf("a site-a envelope whose payload said %s=%q left %d RAW completion row(s) under site-b — the fold keyed the row on the payload's site", claimKey, orientTestSite, got)
			}
			row := orientHeld(t, st, "diaz", orientTestOther)
			if row == nil || row.SiteID != orientTestOther || row.NodeEpoch != 1 || row.Seq != 1 {
				t.Fatalf("the derived row is not the ENVELOPE's (diaz, site-a, epoch 1, seq 1): %+v", row)
			}
			if got := orientRawCount(t, st, "orientation_completion", orientTestOther); got != 1 {
				t.Fatalf("raw completion rows under the envelope's site = %d, want 1", got)
			}

			// The mirror. diaz legitimately holds site-b's (site-b's own
			// envelope); a site-a retraction whose payload claims site-b must
			// leave it standing and act only on site-a's own row.
			w9bApply(t, st, w9bEnvelope(orientTestSite, 1, 1, w9bBase, orientWire(evtOrientationCompleted, "diaz", w9bStr(orientTestPost))))
			if orientHeld(t, st, "diaz", orientTestSite) == nil {
				t.Fatal("FIXTURE DEFECT: site-b's own completion did not land")
			}
			r := orientWire(evtOrientationRetracted, "diaz", by)
			r[claimKey] = orientTestSite
			w9bApply(t, st, w9bEnvelope(orientTestOther, 1, 2, w9bBase+w9bMinute, r))

			if orientHeld(t, st, "diaz", orientTestSite) == nil {
				t.Fatalf("a site-a retraction whose payload said %s=%q REMOVED site-b's derived row — one site marked another's orientation off", claimKey, orientTestSite)
			}
			if got := orientRawCount(t, st, "orientation_retraction", orientTestSite); got != 0 {
				t.Fatalf("the site-a retraction left %d RAW retraction row(s) under site-b", got)
			}
			if orientHeld(t, st, "diaz", orientTestOther) != nil {
				t.Fatal("the site-a retraction did not act on site-a's OWN row — it acted elsewhere, or nowhere")
			}
			if got := orientRawCount(t, st, "orientation_retraction", orientTestOther); got != 1 {
				t.Fatalf("raw retraction rows under the envelope's site = %d, want 1", got)
			}
		})
	}
}

// --- 2. delivery shape and the clock decide nothing -------------------------

// orientDeliveries is every shape two envelopes from two streams can reach a
// post in: each on its own drain, in either order, or on one drain, in
// either order. A fixed slice, iterated in this order at every post ((C1)).
var orientDeliveries = []struct {
	name    string
	batches [][]int // indices into the fixture's [completion, retraction]
}{
	{"two batches, completion first", [][]int{{0}, {1}}},
	{"two batches, retraction first", [][]int{{1}, {0}}},
	{"one batch, completion first", [][]int{{0, 1}}},
	{"one batch, retraction first", [][]int{{1, 0}}},
}

// TestOrientationDerivationFollowsTheKeyUnderEveryDeliveryShapeAndNeverTheClock
// builds the fixture so that the event with the GREATER key carries the
// EARLIER wall clock — a post re-imaged onto epoch 2 whose clock came up ten
// minutes behind. A derivation that reads authored_at, or that lets the last
// delivered event win, gives the opposite answer to one that reads the key.
func TestOrientationDerivationFollowsTheKeyUnderEveryDeliveryShapeAndNeverTheClock(t *testing.T) {
	by := w9bStr(orientTestPost)
	early, late := w9bBase, w9bBase+10*w9bMinute
	fixtures := []struct {
		name   string
		events [2]store.Envelope // [0] completion, [1] retraction
		held   bool
		want   store.Key
	}{
		{
			name: "retraction has the greater key (epoch 2) and the earlier clock — NOT held",
			events: [2]store.Envelope{
				w9bEnvelope(orientTestSite, 1, 1, late, orientWire(evtOrientationCompleted, "diaz", by)),
				w9bEnvelope(orientTestSite, 2, 1, early, orientWire(evtOrientationRetracted, "diaz", by)),
			},
			held: false,
		},
		{
			name: "completion has the greater key (epoch 2) and the earlier clock — held by it",
			events: [2]store.Envelope{
				w9bEnvelope(orientTestSite, 2, 1, early, orientWire(evtOrientationCompleted, "diaz", by)),
				w9bEnvelope(orientTestSite, 1, 1, late, orientWire(evtOrientationRetracted, "diaz", by)),
			},
			held: true,
			want: store.Key{SiteID: orientTestSite, NodeEpoch: 2, Seq: 1},
		},
	}
	tables := []string{"orientation", "orientation_completion", "orientation_retraction"}
	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {
			var base [3][]byte
			for di, d := range orientDeliveries {
				st := w9bNewStore(t)
				for _, batch := range d.batches {
					envs := make([]store.Envelope, 0, len(batch))
					for _, i := range batch {
						envs = append(envs, fx.events[i])
					}
					orientApplyBatch(t, st, envs)
				}
				row := orientHeld(t, st, "diaz", orientTestSite)
				switch {
				case fx.held && row == nil:
					t.Fatalf("%s: NOT held — the completion carries the greater key; the derivation followed the clock or the delivery order", d.name)
				case fx.held && (row.NodeEpoch != fx.want.NodeEpoch || row.Seq != fx.want.Seq):
					t.Fatalf("%s: held by %+v, want the completion at %v", d.name, row, fx.want)
				case !fx.held && row != nil:
					t.Fatalf("%s: HELD %+v — the retraction carries the greater key; the derivation followed the clock or the delivery order", d.name, row)
				}
				var dumps [3][]byte
				for k, name := range tables {
					dumps[k] = orientDump(t, st, name)
				}
				if di == 0 {
					base = dumps
					continue
				}
				for k, name := range tables {
					if !bytes.Equal(dumps[k], base[k]) {
						t.Fatalf("%s: %s dump differs from %q — the derivation is delivery-dependent\n got: %s\nwant: %s", d.name, name, orientDeliveries[0].name, dumps[k], base[k])
					}
				}
			}
		})
	}
}

// --- 3. a refusal leaves no envelope ----------------------------------------

// TestOrientationEveryRefusedEdgeLeavesTheStreamsLogDeepEqualToBefore runs
// every pre-append refusal the two edges can produce and, after each, reads
// the whole owning stream back from the log: same length, same envelopes.
// The watermark-unchanged assertion elsewhere is a proxy; this is the thing.
func TestOrientationEveryRefusedEdgeLeavesTheStreamsLogDeepEqualToBefore(t *testing.T) {
	st := w9bNewStore(t)
	w9bSeedPersons(t, st, orientTestOther, 1, "diaz", "evans")
	if _, err := CompleteOrientation(st, orientTestSite, 1, "diaz", w9bStr(orientTestPost), 0, "default"); err != nil {
		t.Fatal(err)
	}
	before, err := st.Events(orientTestSite, 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 {
		t.Fatalf("FIXTURE DEFECT: site-b's stream holds %d envelopes, want 1", len(before))
	}
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(before[0].Payload, &head); err != nil || head.Type != evtOrientationCompleted {
		t.Fatalf("FIXTURE DEFECT: the one envelope is %q (%v)", head.Type, err)
	}
	seedA, err := st.Events(orientTestOther, 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}

	refusals := []struct {
		name string
		code OrientationRefusalCode
		do   func() error
	}{
		{"duplicate completion", OrientationAlreadyHeld, func() error {
			_, err := CompleteOrientation(st, orientTestSite, 1, "diaz", w9bStr(orientTestPost), 0, "default")
			return err
		}},
		{"bulk with a held id, held id LAST", OrientationAlreadyHeld, func() error {
			_, err := CompleteOrientations(st, orientTestSite, 1, []string{"evans", "diaz"}, w9bStr(orientTestPost), 0, "default")
			return err
		}},
		{"bulk with a repeated id", OrientationAlreadyHeld, func() error {
			_, err := CompleteOrientations(st, orientTestSite, 1, []string{"evans", "evans"}, w9bStr(orientTestPost), 0, "default")
			return err
		}},
		{"retraction of an un-held orientation", OrientationNotHeld, func() error {
			_, err := RetractOrientation(st, orientTestSite, 1, "evans", w9bStr(orientTestPost), 0, "default")
			return err
		}},
		{"cross-site retraction at site-a", OrientationNotHeld, func() error {
			_, err := RetractOrientation(st, orientTestOther, 1, "diaz", w9bStr("Ice Road"), 0, "default")
			return err
		}},
		{"unknown person", OrientationPersonUnknown, func() error {
			_, err := CompleteOrientation(st, orientTestSite, 1, "nobody", nil, 0, "default")
			return err
		}},
		{"unnamed person", OrientationPersonRequired, func() error {
			_, err := RetractOrientation(st, orientTestSite, 1, "", nil, 0, "default")
			return err
		}},
	}
	for _, r := range refusals {
		err := r.do()
		var ref *OrientationRefusal
		if !errors.As(err, &ref) || ref.Code != r.code {
			t.Fatalf("%s: err = %v (%T), want *OrientationRefusal code %v", r.name, err, err, r.code)
		}
		after, err := st.Events(orientTestSite, 1, 1, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(after, before) {
			t.Fatalf("%s: site-b's log CHANGED under a refusal — %d → %d envelopes\nbefore: %+v\n after: %+v", r.name, len(before), len(after), before, after)
		}
		afterA, err := st.Events(orientTestOther, 1, 1, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(afterA, seedA) {
			t.Fatalf("%s: site-a's log CHANGED under a refusal — %d → %d envelopes", r.name, len(seedA), len(afterA))
		}
	}
}
