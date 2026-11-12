package web

// fix2_fg_retired_seams_test.go — cluster G (retired seams) and ruling R4
// (merge carries orientation), fix round 2, 2026-10-04. Each test fails on
// 2ba9188 and passes after; the defect or ruling it pins is in its name.

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// fgReplicate applies every envelope siteID authored in src to dst — the
// sync primitive pair, used here to stage a partition race in one process.
func fgReplicate(t *testing.T, dst, src *store.Store, siteID string) {
	t.Helper()
	evs, err := src.Events(siteID, 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if _, err := dst.Apply(ev); err != nil {
			t.Fatalf("replicating %v: %v", ev.Key(), err)
		}
	}
}

// fgRetireDuringPartition retires personID at a peer ("site-b") that cannot
// see the open visit here, then lets the retirement replicate in: the D8
// state, a person both RETIRED and ON SITE.
func fgRetireDuringPartition(t *testing.T, st *store.Store, personID string) {
	t.Helper()
	peer, err := store.Open(filepath.Join(t.TempDir(), "peer.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { peer.Close() })
	if err := domain.Register(peer); err != nil {
		t.Fatal(err)
	}
	// The peer has the roster, not the check-in: replicate only the
	// person/company/trade creations (the first envelopes of site-a's stream,
	// all authored before checkInCrew's GateCheckIn).
	evs, err := st.Events("site-a", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if strings.Contains(string(ev.Payload), `"gate_check_in"`) || strings.Contains(string(ev.Payload), `"visit`) || strings.Contains(string(ev.Payload), `"trip`) {
			break
		}
		if _, err := peer.Apply(ev); err != nil {
			t.Fatalf("replicating %v to the peer: %v", ev.Key(), err)
		}
	}
	yes := "true"
	if _, err := domain.AmendPerson(peer, "site-b", 1, domain.PersonAmendment{PersonID: personID, Retired: &yes}, 0, "default"); err != nil {
		t.Fatalf("the partitioned peer could not retire %s: %v", personID, err)
	}
	fgReplicate(t, st, peer, "site-b")
}

// D8: someone both retired and on site (a partition race) must still be
// checked out on a vehicle — the whole truck's departure was refused with
// the RETIRED words. Leaving is a presence path and never consults retired.
func TestFix2D8ARetiredPersonOnSiteCanStillLeaveOnAVehicle(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	checkInCrew(t, srv, st, 3)
	crew0 := personNamedOne(t, st, "Crew 00")
	fgRetireDuringPartition(t, st, crew0.PersonID)
	if p, _ := domain.PersonByID(st, crew0.PersonID); p == nil || !p.IsRetired() {
		t.Fatal("precondition: Crew 00 is not retired after the partitioned retire replicated")
	}
	if !fgOpenAt(t, st, "site-a")[crew0.PersonID] {
		t.Fatal("precondition: Crew 00 is not on site")
	}

	vals := a3OutboundBody(t, srv, st, "D8-TRUCK")
	vals.Set("deadline_minutes", "60")
	rec := postForm(t, srv, "/gate/checkout/vehicle", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("D8 REPRODUCED: the departure of a truck carrying a retired-and-on-site person = %d, want 303: %s", rec.Code, w5fixNotices(rec.Body.String(), "notice-critical"))
	}
	if fgOpenAt(t, st, "site-a")[crew0.PersonID] {
		t.Error("the departure was accepted but Crew 00 is still on site")
	}
}

// D8, the typed (scripting-off) path: a departing row naming a retired
// person by name alone adopts that record instead of refusing.
func TestFix2D8ATypedDepartingRowAdoptsTheOnlyRetiredRecord(t *testing.T) {
	retired := "true"
	ret := domain.Person{PersonID: "p-ret", Name: "Ashton, Joss", Retired: &retired}
	slot, err := resolveTypedPersonDir("ashton, joss", []domain.Person{ret}, true)
	if err != nil || slot.ExistingID != "p-ret" {
		t.Fatalf("departing: %+v, %v — want the retired record adopted (D8)", slot, err)
	}
	// Inbound is unchanged: still refused, and in plain words (no record id).
	_, err = resolveTypedPerson("Ashton, Joss", []domain.Person{ret})
	if err == nil || !strings.Contains(err.Error(), "RETIRED") {
		t.Fatalf("inbound: %v — want the retired refusal", err)
	}
	if strings.Contains(err.Error(), "p-ret") {
		t.Errorf("the retired refusal quotes the raw person id (L02): %v", err)
	}
}

// R6: a "different person" decline posted from a tab opened BEFORE the
// namesake was retired arrives as a new name and minted a namesake. It is
// refused at commit; a live namesake still lets a decline through.
func TestFix2R6AStaleDeclineAgainstANowRetiredNameMintsNoNamesake(t *testing.T) {
	srv, st := w5fixServer(t, false)
	shea := orientBlockPerson(t, st, "Branford, Shea")
	w5fixRetire(t, srv, shea.PersonID)

	decline := []passengerRow{{Kind: "new", Value: shea.Name, Typed: shea.Name, CompanyID: newRef, Company: "Boreal Camp Catering"}}
	rec := postForm(t, srv, "/gate/checkin", scriptedCheckInBody("TRK-906", 0, decline))
	if rec.Code == http.StatusSeeOther {
		t.Fatalf("R6: the stale decline was accepted (%s)", rec.Header().Get("Location"))
	}
	if !strings.Contains(rec.Body.String(), "RETIRED") {
		t.Errorf("the refusal does not say the name is retired: %s", w5fixNotices(rec.Body.String(), "notice-critical"))
	}
	if n := len(personNamed(t, srv, shea.Name)); n != 1 {
		t.Errorf("%d roster rows named %q, want 1 — the decline minted a namesake of a retired record", n, shea.Name)
	}

	// Control: with a LIVE namesake on the roster the decline is against a
	// selectable person and still mints, as designed (9f0623b).
	orientBlockPerson(t, st, "Branford, Shea")
	rec = postForm(t, srv, "/gate/checkin", scriptedCheckInBody("TRK-907", 0, decline))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("a decline against a live namesake = %d, want 303: %s", rec.Code, w5fixNotices(rec.Body.String(), "notice-critical"))
	}
	if n := len(personNamed(t, srv, shea.Name)); n != 3 {
		t.Errorf("%d roster rows named %q after the live-namesake decline, want 3", n, shea.Name)
	}
}

// L05: an /orientation form opened before a retire still recorded the tick.
func TestFix2L05AStaleOrientationTickForARetiredPersonRecordsNothing(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	w5fixRetire(t, w.srv, w.uma.PersonID)
	rec := postForm(t, w.srv, "/orientation/complete", url.Values{"person_id": {w.uma.PersonID}})
	if rec.Code == http.StatusSeeOther {
		t.Fatalf("L05 REPRODUCED: the stale tick for a retired person was recorded (%s)", rec.Header().Get("Location"))
	}
	if !strings.Contains(rec.Body.String(), "retired") {
		t.Errorf("the refusal does not say why: %s", rec.Body.String())
	}
	if held, err := domain.OrientationHeld(w.st, w.uma.PersonID, "site-b"); err != nil || held != nil {
		t.Errorf("an orientation row exists for the retired person: %+v, %v", held, err)
	}
}

// R4: merging two person records — the canonical record inherits the
// merged-away record's orientation, visibly, and reversibly on unmerge.
func TestFix2R4MergeCarriesOrientationVisiblyAndReversibly(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	cookie := loginCookie(t, w.srv, testLoginPassword)
	// Orla holds Pad's orientation; Uma does not. Orla's record is the
	// duplicate: merge it into Uma.
	if rec := postForm(t, w.srv, "/person/amend", url.Values{"person_id": {w.orla.PersonID}, "same_as": {w.uma.PersonID}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("merge = %d: %s", rec.Code, rec.Body.String())
	}
	merge, err := domain.LoadPersonMergeMap(w.st)
	if err != nil {
		t.Fatal(err)
	}
	h, err := domain.OrientationHeldThroughMerge(w.st, merge, w.uma.PersonID, "site-b")
	if err != nil || h == nil || h.CarriedFrom != w.orla.PersonID {
		t.Fatalf("R4: the canonical record does not inherit the merged-away record's orientation: %+v, %v", h, err)
	}
	// Visibly: the Holding list names Uma and where it came from.
	page := w2Get(t, w.srv, "/orientation", cookie).Body.String()
	holding := sectionBetween(t, page, "Hold Pad Site Control's orientation", "</table>")
	if !strings.Contains(holding, w.uma.Name) || !strings.Contains(holding, "carried over from merged record "+w.orla.Name) {
		t.Errorf("the Holding list does not show Uma's inherited orientation and its source:\n%s", holding)
	}
	// The commit enters her: not held out.
	if rec := postForm(t, w.srv, "/gate/checkin", scriptedCheckInBody("TRK-R4", 0, orientBlockRows(w.uma))); rec.Code != http.StatusSeeOther {
		t.Fatalf("check-in = %d: %s", rec.Code, rec.Body.String())
	}
	if !orientBlockOpenPersonIDs(t, w.st)[w.uma.PersonID] {
		t.Error("R4: the canonical record was held out although the merged-away record holds the orientation")
	}
	// Reversibly: un-merge, and the inheritance is gone with no retraction.
	if rec := postForm(t, w.srv, "/person/amend", url.Values{"person_id": {w.orla.PersonID}, "clear": {"same_as"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("un-merge = %d: %s", rec.Code, rec.Body.String())
	}
	merge, _ = domain.LoadPersonMergeMap(w.st)
	if h, err := domain.OrientationHeldThroughMerge(w.st, merge, w.uma.PersonID, "site-b"); err != nil || h != nil {
		t.Errorf("after the un-merge Uma still holds the orientation: %+v, %v", h, err)
	}
	if held, _ := domain.OrientationHeld(w.st, w.orla.PersonID, "site-b"); held == nil {
		t.Error("the un-merge lost Orla's own orientation row")
	}
}

func personNamedOne(t *testing.T, st *store.Store, name string) domain.Person {
	t.Helper()
	all, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range all {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("nobody named %q", name)
	return domain.Person{}
}

func fgOpenAt(t *testing.T, st *store.Store, site string) map[string]bool {
	t.Helper()
	vs, err := domain.OpenVisitsAt(st, site)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, v := range vs {
		out[v.PersonID] = true
	}
	return out
}
