// witness_u8_dupadvisory_test.go — witness phase, u8-post19 assignment
// (reports/witness/u8-post19.md), secondary to this phase's unsolicited-
// arrival work (witness_u8_post19_test.go). Kills six post-19eafc4 census
// survivors in internal/web/dupadvisory.go (reports/witness/u2-census-rerun.md,
// post19_unreached.tsv) — a file with ZERO test references anywhere in the
// suite before this one, so every block in it was unreached, not just
// these six:
//
//	dupadvisory.go:99-102   Server.dupAdvisoryPersonIDs  `if err != nil {`  (domain.Persons fault)
//	dupadvisory.go:104-107  Server.dupAdvisoryPersonIDs  `if err != nil {`  (domain.Companies fault)
//	dupadvisory.go:120-121  Server.dupAdvisoryPersonIDs  `if seen[id] {`   (cross-slot dedup)
//	dupadvisory.go:142-144  dupWarnRedirect              `if strings.Contains(target, "?") {`
//	dupadvisory.go:164-165  dupWarningsFrom              `if seen[id] {`   (repeated query id)
//	dupadvisory.go:169-170  dupWarningsFrom              `if !ok {`        (unrecognised query id)
package web

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// u8DropTable forces a targeted read failure the way witness_y8_tripauthor_test.go's
// y8DropTable does in package domain — LocalTx, the sanctioned read/write
// door, dropping one table. The store is always a fresh t.TempDir() one
// discarded at test end, so nothing is ever restored mid-test.
func u8DropTable(t *testing.T, st *store.Store, table string) {
	t.Helper()
	if err := st.LocalTx(func(tx *sql.Tx) error {
		_, err := tx.Exec("DROP TABLE " + table)
		return err
	}); err != nil {
		t.Fatalf("fixture defect: drop table %s: %v", table, err)
	}
}

// TestWitnessU8DupAdvisoryPersonsFaultIsSwallowedNotPanicked kills
// dupadvisory.go:99-102. The advisory's own contract (its doc comment) is
// "loud on failure, never fatal" — a lookup failure here must not cost the
// officer their check-in. Dropping `person` (what domain.Persons reads)
// forces exactly this branch.
//
// ⛔ WEAK-WITNESS TRAP MEASURED HERE, NOT ASSUMED: an empty `got` alone does
// NOT distinguish this branch existing from it being deleted. Deleting the
// early return leaves `persons` nil (Persons returns (nil, err) on
// failure), and duplicateCandidatePersonIDs ranges over that nil slice
// finding nothing regardless — so a plant that removes this whole `if`
// still produces `len(got) == 0`, the SAME observable result, verified live
// by applying exactly that plant before writing this comment. The only
// actual behavioural difference the branch makes is "loud" (the log line)
// — so the assertion has to be on the log line the branch's own doc comment
// promises, using the same log-capture control (w5a_read_paths_test.go's
// w5aCaptureWebLog) this package already established.
func TestWitnessU8DupAdvisoryPersonsFaultIsSwallowedNotPanicked(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	u8DropTable(t, st, "person")
	logbuf := w5aCaptureWebLog(t)

	got := srv.dupAdvisoryPersonIDs(domain.PersonSlot{NewName: "Dana Driver"}, nil)
	if len(got) != 0 {
		t.Fatalf("dupAdvisoryPersonIDs with a broken person read = %v, want nil/empty — a fault here must "+
			"read as 'no advisory', never a match or a crash", got)
	}
	if !strings.Contains(logbuf.String(), "gate: dup advisory: persons:") {
		t.Fatalf("expected the persons-read fault to be logged (\"gate: dup advisory: persons: ...\"), got log: %q",
			logbuf.String())
	}
}

// TestWitnessU8DupAdvisoryCompaniesFaultIsSwallowedNotPanicked kills
// dupadvisory.go:104-107 — the SECOND fault branch, distinct from the one
// above: `person` reads fine, `company` does not. Unlike the persons-fault
// arm, this one IS distinguishable by return value alone, verified live: if
// this `if` is deleted, `companyName` falls back to an empty (not nil)
// map and execution carries on using the REAL persons list — and since
// this fixture's slot types no company at all, duplicateCandidatePersonIDs
// never consults companyName and still finds the "Dana Driver" name match,
// so the plant flips `got` from empty to non-empty. The log assertion is
// kept anyway for the same "loud on failure" contract the branch documents,
// and because a future fixture that adds NewCompany would silently fall
// back to the weak, return-value-only shape.
func TestWitnessU8DupAdvisoryCompaniesFaultIsSwallowedNotPanicked(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	u8DropTable(t, st, "company")
	logbuf := w5aCaptureWebLog(t)

	got := srv.dupAdvisoryPersonIDs(domain.PersonSlot{NewName: "Dana Driver"}, nil)
	if len(got) != 0 {
		t.Fatalf("dupAdvisoryPersonIDs with a broken company read = %v, want nil/empty", got)
	}
	if !strings.Contains(logbuf.String(), "gate: dup advisory: companies:") {
		t.Fatalf("expected the companies-read fault to be logged (\"gate: dup advisory: companies: ...\"), got log: %q",
			logbuf.String())
	}
}

// TestWitnessU8DupAdvisoryDedupsTheSameMatchAcrossSlots kills
// dupadvisory.go:120-121. Two DIFFERENT off-roster slots on the same
// vehicle (the driver and an occupant) that both happen to type the same
// existing roster name must surface as ONE advisory candidate, not two —
// dupWarnRedirect below encodes ids as repeated query params, and without
// this dedup the same person would ride the redirect twice and (per
// dupWarningsFrom's OWN dedup, tested separately below) still only render
// once — so this specific duplication is invisible downstream and only
// this test catches it directly, at the source.
func TestWitnessU8DupAdvisoryDedupsTheSameMatchAcrossSlots(t *testing.T) {
	srv, _, persons := newGateTestServer(t)
	driver := domain.PersonSlot{NewName: "Dana Driver"}
	occupant := domain.PersonSlot{NewName: "Dana Driver"} // same typo/duplicate, independently typed

	got := srv.dupAdvisoryPersonIDs(driver, []domain.PersonSlot{occupant})
	if len(got) != 1 {
		t.Fatalf("dupAdvisoryPersonIDs across two slots matching the same roster person = %v (len %d), want "+
			"exactly 1 — the cross-slot seen-map must dedup by id", got, len(got))
	}
	var wantID string
	for _, p := range persons {
		if p.Name == "Dana Driver" {
			wantID = p.PersonID
		}
	}
	if wantID == "" {
		t.Fatal("fixture defect: no roster person named Dana Driver")
	}
	if got[0] != wantID {
		t.Fatalf("matched id = %q, want the roster's Dana Driver (%q)", got[0], wantID)
	}
}

// TestWitnessU8DupWarnRedirectAppendsWithAmpersandWhenTargetHasAQuery kills
// dupadvisory.go:142-144. dupWarnRedirect is chainable with timeWarnRedirect
// per its own doc comment — the realistic caller shape in gate.go is
// `dupWarnRedirect(timeWarnRedirect(...), dupIDs)`, so the target it
// receives routinely already carries a "?". Get the separator wrong and the
// redirect either double-`?`s (a malformed URL some routers reject with a
// generic error the officer cannot act on) or silently drops the dup query
// as part of another param's value.
func TestWitnessU8DupWarnRedirectAppendsWithAmpersandWhenTargetHasAQuery(t *testing.T) {
	got := dupWarnRedirect("/gate?warn=late-arrival", []string{"site-a-person-1"})
	// ⟦W3 G-049, 2026-10-03⟧ ⟦was: want == "/gate?warn=late-arrival&dup=site-a-person-1"⟧
	// the redirect is now re-encoded and sealed (noticeseal.go), so the exact
	// string changed; the property this pins — one well-formed query keeping
	// BOTH params — is asserted by parsing it.
	u, err := url.Parse(got)
	if err != nil || strings.Count(got, "?") != 1 || u.Path != "/gate" ||
		u.Query().Get("warn") != "late-arrival" || u.Query().Get("dup") != "site-a-person-1" || u.Query().Get(noticeSealQueryKey) == "" {
		t.Fatalf("dupWarnRedirect(%q, ...) = %q, want one query carrying warn, dup and the seal", "/gate?warn=late-arrival", got)
	}
}

// TestWitnessU8DupWarningsFromDedupsRepeatedQueryIDs kills
// dupadvisory.go:164-165. A `dup` id repeated on the query string (a
// resubmitted/back-button redirect, or two slots that matched the same
// person before this file's own 120-121 dedup existed) must render ONE
// notice, not one per occurrence — the officer would otherwise read the
// same sentence about the same person twice on one page.
func TestWitnessU8DupWarningsFromDedupsRepeatedQueryIDs(t *testing.T) {
	// ⟦W3 G-049⟧ sealed, as a real redirect is — an unsealed query renders nothing.
	req := httptest.NewRequest(http.MethodGet, sealNotices("/gate?dup=site-a-person-1&dup=site-a-person-1"), nil)
	personName := map[string]string{"site-a-person-1": "Dana Driver"}

	got := dupWarningsFrom(req, personName)
	if len(got) != 1 {
		t.Fatalf("dupWarningsFrom with the same id repeated on the query = %v (len %d), want exactly 1", got, len(got))
	}
	if want := dupWarningCopy("Dana Driver"); got[0] != want {
		t.Fatalf("warning = %q, want %q", got[0], want)
	}
}

// TestWitnessU8DupWarningsFromRendersNothingForAnUnrecognisedID kills
// dupadvisory.go:169-170. An id on the query that personName does not
// resolve — the roster changed, or (per the file's own doc) an id that is
// simply request surface rather than a message channel — must render
// nothing for that id, never a warning with a blank/zero-value name.
func TestWitnessU8DupWarningsFromRendersNothingForAnUnrecognisedID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/gate?dup=site-a-person-does-not-exist", nil)
	personName := map[string]string{"site-a-person-1": "Dana Driver"}

	got := dupWarningsFrom(req, personName)
	if len(got) != 0 {
		t.Fatalf("dupWarningsFrom for an unrecognised id = %v, want nil/empty — never a warning naming nobody", got)
	}
}
