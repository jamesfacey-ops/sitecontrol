// d80_close_never_aboard_test.go — [[D-80]] (2026-09-05): the "never aboard"
// correction on a departed trip is CLOSED, exactly as [[D-78]] closed cancel.
//
// James, verbatim: "they would not mark as never aboard they would just
// remove them from the list the same way as if they left" / "no one would be
// standing at a site without a vehicle as its -65F" / "Yes — close it, same
// as cancel." The situation the operation served — a person on the manifest
// who was never in the vehicle — does not occur; its only real effect was a
// second door onto the person-on-no-list hole: the origin visit closed at
// departure, the correction may not reopen it, and the removed person lands
// on no sheet at either post.
//
// THE MECHANISM, and why it is not cancel's. Cancel was a whole route with
// one effect, so the route was deleted. Never-aboard is ONE FIELD
// (occupants_remove) on POST /trips/amend, a route that carries six other
// legitimate corrections — and the hole is a property of the PERSON, not
// the trip: it opens exactly when the person has no recorded sighting since
// the trip left, so the manifest is the only place they are accounted for.
// That predicate already exists — domain.UnsightedRemovals, built on the
// same sightedSince the muster's four categories share — and F4 §7 used it
// to WARN. D-80 turns the warning into a REFUSAL with no confirm bypass. A
// SIGHTED person's removal (they are on a sheet by their visit) is
// record-only and still authors, so the closure cannot strand anyone and
// cannot refuse a correction of someone demonstrably elsewhere. The domain
// author and the fold are untouched: already-replicated removals still
// compose (T2), and the tests that pin that author them directly.
//
// WHAT THIS FILE PROVES: ⭐ a person removed — or attempted to be removed —
// from a departed trip's manifest cannot end up on no sheet at either post,
// through every state the trip passes (open; arrived with a stayer; after a
// sighted removal), at BOTH posts, with and without the retired confirm
// flag. ⟦was: "CONTROLLED: the same removal authored directly through the
// domain (the retained state, what the old path did) DOES take the stayer
// off every sheet — so the sheets can show the hole"⟧ — James, 2026-10-04:
// people in raw∖effective are listed in NotTracked from the moment the
// correction is recorded (FINDINGS-R4 D1 ruling), so that direct removal now
// leaves the stayer on the NOT TRACKED sheet; step 5 pins that instead. ⚠ Nothing here rests on a status code; the store and the
// rendered sheets are the witnesses.
package web

import (
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// d80Sheets renders the printed muster at both posts, style-stripped.
func d80Sheets(t *testing.T, srvA, srvB *Server) (a, b string) {
	t.Helper()
	return styleRe.ReplaceAllString(rawPage(t, srvA, "/muster/print"), ""),
		styleRe.ReplaceAllString(rawPage(t, srvB, "/muster/print"), "")
}

// d80InSection reports whether name is printed inside the named muster
// section (nesting-aware: the not-tracked section sits inside away-scope).
func d80InSection(page, section, name string) bool {
	at := strings.Index(page, `data-muster-section="`+section+`"`)
	if at < 0 {
		return false
	}
	depth, i := 1, at
	for depth > 0 {
		open := strings.Index(page[i+1:], "<section")
		close := strings.Index(page[i+1:], "</section>")
		if close < 0 {
			return strings.Contains(page[at:], name)
		}
		if open >= 0 && open < close {
			depth++
			i += 1 + open
		} else {
			depth--
			i += 1 + close
		}
	}
	return strings.Contains(page[at:i], name)
}

// d80AttemptRemoval posts the old never-aboard body at srv — first bare,
// then with the retired confirm_unsighted=1 a stale form could still carry —
// and reports whether EITHER authored, with the last page for the message.
func d80AttemptRemoval(t *testing.T, srv *Server, st *store.Store, tripID, personID string) (authored bool, page string) {
	t.Helper()
	for _, confirm := range []string{"", "1"} {
		form := url.Values{w9aFieldTripID: {tripID}, w9aFieldOccupantsRemove: {personID}}
		if confirm != "" {
			form.Set(w9aFieldConfirmUnsighted, confirm)
		}
		p, ok := w9aAmend(t, srv, st, w9aTripAmendPath, form)
		page = p
		if ok {
			return true, page
		}
	}
	return false, page
}

// TestD80NobodyRemovedFromADepartedTripLandsOnNoSheetAtEitherPost — the
// witness D-80 asked for.
func TestD80NobodyRemovedFromADepartedTripLandsOnNoSheetAtEitherPost(t *testing.T) {
	w := newRoadWorld(t) // site-a: TRK-100 → site-b with Nadeau (driver), Ohara, Petrov aboard
	w.srv.Cfg.LoginPassword = testLoginPassword
	srvB := New(w3ConfigSiteB(w.st.Path), w.st) // the destination post, over the SAME store
	ohara, petrov := w.riding[1], w.riding[2]

	everyoneOnASheet := func(step string) {
		t.Helper()
		a, b := d80Sheets(t, w.srv, srvB)
		for _, p := range w.riding {
			if !strings.Contains(a, p.Name) && !strings.Contains(b, p.Name) {
				t.Fatalf("D-80 [%s]: %s is on NO sheet at either post — the person-on-no-list hole.\n--- site-a ---\n%s\n--- site-b ---\n%s", step, p.Name, a, b)
			}
		}
	}
	everyoneOnASheet("precondition: three on the road")

	// --- 1. OPEN trip: every rider is unsighted, so the correction is refused at BOTH posts.
	for _, post := range []struct {
		name string
		srv  *Server
	}{{"origin", w.srv}, {"destination", srvB}} {
		if authored, page := d80AttemptRemoval(t, post.srv, w.st, w.trip.TripID, petrov.PersonID); authored {
			t.Fatalf("D-80: removing %s from the OPEN trip at the %s AUTHORED — that is the old never-aboard path, closed by D-80. Page:\n%s", petrov.Name, post.name, page)
		}
	}
	a, _ := d80Sheets(t, w.srv, srvB)
	if !strings.Contains(a, `data-tally-count="in-transit">3<`) {
		t.Fatalf("D-80: the origin's road headcount is no longer 3 after the refused removals:\n%s", a)
	}
	everyoneOnASheet("after the refused removals on the open trip")

	// --- 2. The trip arrives at site-b; Ohara stayed behind. She is now on the NOT TRACKED sheet, by the manifest alone.
	if _, err := domain.RecordExpectedArrivalExcluding(w.st, "site-b", 1, w.trip.TripID, []string{ohara.PersonID}, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	everyoneOnASheet("after arrival with Ohara stayed behind")
	a, b := d80Sheets(t, w.srv, srvB)
	if !d80InSection(a, "not-tracked", ohara.Name) && !d80InSection(b, "not-tracked", ohara.Name) {
		t.Fatalf("precondition: %s (stayed behind) is not in NOT TRACKED at either post — then step 3 would prove nothing.\n--- site-a ---\n%s\n--- site-b ---\n%s", ohara.Name, a, b)
	}

	// --- 3. Ohara is UNSIGHTED: the manifest is the only place she is listed. Refused at both posts; she stays on the sheet.
	for _, post := range []struct {
		name string
		srv  *Server
	}{{"origin", w.srv}, {"destination", srvB}} {
		if authored, page := d80AttemptRemoval(t, post.srv, w.st, w.trip.TripID, ohara.PersonID); authored {
			t.Fatalf("D-80: removing the stayed-behind rider %s at the %s AUTHORED — she would have left NOT TRACKED and landed on no sheet. Page:\n%s", ohara.Name, post.name, page)
		}
	}
	everyoneOnASheet("after the refused removals of the stayed-behind rider")

	// --- 4. CONTROL that the closure is not blanket: Petrov is SIGHTED (B's arrival minted his visit), so his removal is record-only and AUTHORS — and he stays on B's on-site sheet.
	if authored, page := d80AttemptRemoval(t, srvB, w.st, w.trip.TripID, petrov.PersonID); !authored {
		t.Fatalf("D-80 CONTROL: removing a SIGHTED occupant from the arrived trip was refused — the closure has become a blanket refusal of record-only corrections. Page:\n%s", page)
	}
	everyoneOnASheet("after the sighted removal")
	if _, b = d80Sheets(t, w.srv, srvB); !d80InSection(b, "on-site", petrov.Name) {
		t.Fatalf("D-80: %s left the destination's ON SITE sheet when his manifest row was removed — an amendment moved someone on the muster (F4 §7 forbids it).\n%s", petrov.Name, b)
	}

	// --- 5. The OLD path, authored directly through the domain (the retained state).
	// James, 2026-10-04: people in raw∖effective are listed in NotTracked from the moment the
	// correction is recorded (FINDINGS-R4 D1 ruling). ⟦was: a CONTROL asserting the stayer then
	// vanished from every sheet at both posts — "so the sheets can show the hole"⟧. The read no
	// longer has that hole: a removal that gets past the D-80 gate (a direct domain author, or a
	// removal replicated from a divergent writer) now lands the person on the NOT TRACKED sheet as
	// "taken off this run by a trip correction". The refusals above are therefore no longer the
	// only thing between a removal and the hole — they remain the officer's legible answer — and
	// this step now pins the second line of defence instead of the hole it closed.
	if _, err := domain.AmendTrip(w.st, "site-a", 1, domain.TripAmendment{TripID: w.trip.TripID, OccupantsRemove: []string{ohara.PersonID}}, 0, "default"); err != nil {
		t.Fatalf("D-80: the direct domain removal was refused: %v", err)
	}
	everyoneOnASheet("after a direct domain removal of the stayed-behind rider")
	if a, b = d80Sheets(t, w.srv, srvB); !d80InSection(a, "not-tracked", ohara.Name) || !d80InSection(b, "not-tracked", ohara.Name) ||
		!strings.Contains(a, "taken off this run by a trip correction") || !strings.Contains(b, "taken off this run by a trip correction") {
		t.Fatalf("D-80 / D1 ruling: after a direct domain removal the stayed-behind rider %s is not on the NOT TRACKED sheet at BOTH posts as taken off by a trip correction.\n--- site-a ---\n%s\n--- site-b ---\n%s", ohara.Name, a, b)
	}
}
