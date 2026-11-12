// fable_ra1_witness_test.go — R-A1 independent re-derivation probes
// (reports/witness/FABLE-RA1-OWNMINTED.md). NOT a shipped test: written in
// an audit worktree to MEASURE, at the funnel, what the classification
// table and refuseForeignRow actually do in the fully-registered
// configuration (domain.Register wired), including the two early-return
// branches that pass without consulting the table.
package domain

import (
	"errors"
	"fmt"
	"testing"

	"sitecontrol/internal/store"
)

// ra1Replicate replays every camp-9 event held by src into dst (idempotent:
// duplicates are (false, nil)).
func ra1Replicate(t *testing.T, src, dst *store.Store, site string) {
	t.Helper()
	envs, err := src.Events(site, 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dst.ApplyBatch(envs); err != nil {
		t.Fatalf("replicate %s: %v", site, err)
	}
}

// Q2 (a): fully registered, a type absent from the whole table is REFUSED
// with the typed error and nothing is appended.
func TestFableRA1_FullyRegisteredFunnelRefusesAnUnclassifiedType(t *testing.T) {
	st := newDomainTestStore(t)
	_, err := st.Author("camp-1", 1, []byte(`{"type":"fable_probe_unclassified","x":1}`), 0, "default")
	var unreg *store.UnregisteredTypeError
	if !errors.As(err, &unreg) {
		t.Fatalf("want *store.UnregisteredTypeError, got %v (%T)", err, err)
	}
	if unreg.EventType != "fable_probe_unclassified" || unreg.SiteID != "camp-1" {
		t.Fatalf("refusal names the wrong thing: %+v", *unreg)
	}
	if wm := d87Watermark(t, st, "camp-1"); wm != 0 {
		t.Fatalf("watermark advanced to %d on a refusal", wm)
	}
}

// Q2 (b): the RESIDUAL fail-open — a payload with no `type` key passes the
// funnel untouched and lands in the replicated log. Nothing in the
// classification table is consulted for it.
func TestFableRA1_TypelessObjectPassesTheFullyRegisteredFunnel(t *testing.T) {
	st := newDomainTestStore(t)
	env, err := st.Author("camp-1", 1, []byte(`{"person_id":"nobody","note":"no type key at all"}`), 0, "default")
	if err != nil {
		t.Fatalf("a typeless object was refused: %v — if this now fails, the residual fail-open has been closed", err)
	}
	if wm := d87Watermark(t, st, "camp-1"); wm != 1 {
		t.Fatalf("watermark %d, want 1", wm)
	}
	got, err := st.Events("camp-1", 1, 1, 0)
	if err != nil || len(got) != 1 || got[0].Seq != env.Seq {
		t.Fatalf("event_log does not hold the typeless row: %v %v", got, err)
	}
	t.Logf("MEASURED: typeless object landed at %v with payload %s", env.Key(), got[0].Payload)
}

// Q2 (c): non-object payloads. Validate accepts any valid JSON; the funnel's
// head-decode fails or yields "" and returns nil. What then happens is the
// folds' business — measured here, not assumed.
func TestFableRA1_NonObjectPayloadsAtTheFullyRegisteredFunnel(t *testing.T) {
	for i, raw := range []string{
		`"orientation_completed"`,
		`[]`,
		`null`,
		`42`,
		`[{"type":"visit_checked_out","visit_id":"x"}]`,
		`{"type":""}`,
		`{"type":null}`,
	} {
		st := newDomainTestStore(t)
		_, err := st.Author("camp-1", 1, []byte(raw), 0, "default")
		wm := d87Watermark(t, st, "camp-1")
		var unreg *store.UnregisteredTypeError
		var own *store.OwnershipError
		switch {
		case errors.As(err, &unreg), errors.As(err, &own):
			t.Errorf("probe %d %s: the funnel REFUSED it (%T) — the early return did not fire as read", i, raw, err)
		case err != nil:
			t.Logf("MEASURED probe %d %s: passed the funnel, then FAILED downstream: %v (watermark %d)", i, raw, err, wm)
			if wm != 0 {
				t.Errorf("probe %d: error returned but watermark advanced to %d", i, wm)
			}
		default:
			t.Logf("MEASURED probe %d %s: passed the funnel AND LANDED (watermark %d)", i, raw, wm)
			if wm != 1 {
				t.Errorf("probe %d: no error but watermark %d", i, wm)
			}
		}
	}
}

// Q2 (d): decoder-shape probes against a FOREIGN presence row. The funnel
// decodes `type` into a Go struct (case-insensitive key match, last
// duplicate wins); the resolver reads the payload as an exact-key map.
// Every shape below must be refused — the conservative direction.
func TestFableRA1_CaseAndDuplicateKeyProbesAgainstAForeignVisit(t *testing.T) {
	p := d87TwoPosts(t)
	probes := []struct {
		name, payload, wantOwner string
	}{
		{"capitalised Type key", fmt.Sprintf(`{"Type":"visit_checked_out","visit_id":%q}`, p.visitID), "camp-9"},
		{"upper-case keys throughout", fmt.Sprintf(`{"TYPE":"visit_checked_out","VISIT_ID":%q}`, p.visitID), ""},
		{"duplicate type, gated last", fmt.Sprintf(`{"type":"person_amended","type":"visit_checked_out","visit_id":%q}`, p.visitID), "camp-9"},
	}
	for _, pr := range probes {
		before := d87Watermark(t, p.st7, "camp-7")
		_, err := p.st7.Author("camp-7", 1, []byte(pr.payload), 0, "default")
		var own *store.OwnershipError
		if !errors.As(err, &own) {
			t.Errorf("%s: want *store.OwnershipError, got %v (%T)", pr.name, err, err)
			continue
		}
		if own.TargetSiteID != pr.wantOwner || own.SiteID != "camp-7" {
			t.Errorf("%s: refusal %+v, want TargetSiteID %q", pr.name, *own, pr.wantOwner)
		}
		if after := d87Watermark(t, p.st7, "camp-7"); after != before {
			t.Errorf("%s: watermark moved %d -> %d on a refusal", pr.name, before, after)
		}
	}

	// The reverse duplicate: gated type FIRST, ungated LAST. Both the funnel
	// and every fold take the last value, so this must NOT close camp-9's
	// visit at camp-7 whatever else happens to it.
	rev := fmt.Sprintf(`{"type":"visit_checked_out","type":"person_amended","visit_id":%q}`, p.visitID)
	_, err := p.st7.Author("camp-7", 1, []byte(rev), 0, "default")
	t.Logf("MEASURED reverse-duplicate probe: err=%v", err)
	open, err := OpenVisitsAt(p.st7, "camp-9")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].VisitID != p.visitID {
		t.Fatalf("camp-9's visit at camp-7 is no longer the one open visit: %+v", open)
	}
}

// Q3: orientation, hand-crafted at the FUNNEL (bypassing the edge). A
// foreign retraction passes the funnel (ownMinted → acknowledged) and is
// INERT: the derivation keys on env.SiteID. An injected site_id in a
// completion is ignored: the row mints under the AUTHORING post.
func TestFableRA1_OrientationCrossSiteProbesAtTheFunnel(t *testing.T) {
	p := d87TwoPosts(t)
	if _, err := CompleteOrientation(p.st9, "camp-9", 1, p.personID, nil, 0, "default"); err != nil {
		t.Fatalf("fixture: camp-9 completes orientation: %v", err)
	}
	ra1Replicate(t, p.st9, p.st7, "camp-9")
	held := func(st *store.Store, site string) bool {
		r, err := OrientationHeld(st, p.personID, site)
		if err != nil {
			t.Fatal(err)
		}
		return r != nil
	}
	if !held(p.st7, "camp-9") {
		t.Fatal("fixture: camp-7 must HOLD camp-9's orientation row after replication")
	}

	// (1) camp-7 hand-crafts a retraction naming the person camp-9 oriented.
	env, err := p.st7.Author("camp-7", 1, []byte(fmt.Sprintf(`{"type":"orientation_retracted","person_id":%q}`, p.personID)), 0, "default")
	if err != nil {
		t.Fatalf("the funnel refused an ownMinted type: %v", err)
	}
	t.Logf("MEASURED: foreign-shaped retraction landed at %v (funnel passes ownMinted)", env.Key())
	if !held(p.st7, "camp-9") {
		t.Fatal("DEFECT: camp-7's retraction removed camp-9's (person, camp-9) row at camp-7")
	}
	if held(p.st7, "camp-7") {
		t.Fatal("a dangling retraction produced a camp-7 row")
	}

	// (2) camp-7 hand-crafts a completion with an INJECTED site_id naming camp-9.
	if _, err := p.st7.Author("camp-7", 1, []byte(fmt.Sprintf(`{"type":"orientation_completed","person_id":%q,"site_id":"camp-9"}`, p.personID)), 0, "default"); err != nil {
		t.Fatalf("the funnel refused an ownMinted type: %v", err)
	}
	if !held(p.st7, "camp-7") {
		t.Fatal("the injected completion did not mint under the AUTHORING post camp-7")
	}
	if r, _ := OrientationHeld(p.st7, p.personID, "camp-9"); r == nil || r.NodeEpoch != 1 || r.SiteID != "camp-9" {
		t.Fatalf("camp-9's row changed under an injected completion: %+v", r)
	}

	// (3) the other post sees the same: replicate camp-7's stream into camp-9.
	ra1Replicate(t, p.st7, p.st9, "camp-7")
	if !held(p.st9, "camp-9") {
		t.Fatal("DEFECT: camp-9's own orientation row was removed at camp-9 by camp-7's wire retraction")
	}
	if !held(p.st9, "camp-7") {
		t.Fatal("camp-9 does not hold camp-7's minted row after replication")
	}
}
