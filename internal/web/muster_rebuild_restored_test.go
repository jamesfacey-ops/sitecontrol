// muster_rebuild_restored_test.go — R-23 (James, 2026-09-13: "Restore them,
// but mark them as restored") through the camp harness: two real posts, a
// real wipe-and-re-image, the real boot Rejoin, the rendered sheet.
//
// NEW FILE beside muster_rebuild_test.go, per that file's tracking rule
// (design/app-fixes-v1.md §5); the camp harness (newCamp, wipeAndReimage,
// renderMuster, settle) is reused from it, not duplicated. The five tests
// in it still assert that RECOVERY WORKS — every pre-wipe name comes back
// and is counted. This file asserts what the sheet now SAYS about those
// names: every one recovered from the peer's copy carries the RESTORED tag
// and the count line names how many; a crew that arrives at the rebuilt
// post afterwards — observed by THIS incarnation — does not. Second-hand
// presence is never presented as first-hand.
package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// restoredTagged returns the names whose <li> carries the RESTORED tag in
// the on-site section — the eye for the marking, beside nameLiRe's eye for
// the names.
func restoredTagged(t *testing.T, s sheet) []string {
	t.Helper()
	re := regexp.MustCompile(`<li>([^<]+?)\s*<span class="restored-tag"[^>]*>RESTORED</span></li>`)
	var out []string
	for _, m := range re.FindAllStringSubmatch(s.section(t, "on-site"), -1) {
		out = append(out, strings.TrimSpace(m[1]))
	}
	return out
}

func TestRebuiltPadMarksEveryRecoveredNameRestoredAndNoneItObservedItself(t *testing.T) {
	c := newCamp(t)
	recovered := append(append([]string{}, names(c.arrivedFromA)...), names(c.localAtB)...)

	// Before the wipe nothing on the Pad's sheet is second-hand: it saw
	// every one of them walk in.
	before := renderMuster(t, c.b)
	if tagged := restoredTagged(t, before); len(tagged) != 0 {
		t.Fatalf("precondition: the un-rebuilt Pad tags %v RESTORED — nothing it holds under its own tag is second-hand", tagged)
	}
	if strings.Contains(before.body, `data-restored-count=`) {
		t.Fatalf("precondition: the un-rebuilt Pad's sheet carries a restored count")
	}

	// The rebuild: deploy/wipe-replay-siteb.ps1, in-process. Boot Rejoin
	// recovers the Pad's own epoch-1 history from Ice Road's copy.
	c.b = wipeAndReimage(t, c.b)
	after := renderMuster(t, c.b)

	// "Restore them": recovery is whole — asserted by the tracked five too,
	// repeated here so this file's marking claims cannot pass on a sheet
	// that recovered nobody.
	if got := after.tally(t, "on-site"); got != len(recovered) {
		t.Fatalf("the rebuilt Pad reads %d on site, want %d — recovery must still restore the whole shift", got, len(recovered))
	}
	gotNames, _, _ := after.roster(t)
	if !sortedEqual(gotNames, recovered) {
		t.Fatalf("the rebuilt Pad's roster names %v, want %v", gotNames, recovered)
	}
	// "…but mark them as restored": every recovered name, and a count.
	if tagged := restoredTagged(t, after); !sortedEqual(tagged, recovered) {
		t.Errorf("R-23: the rebuilt Pad tags %v RESTORED, want every recovered name %v — a name it never saw walk in must not read as first-hand", tagged, recovered)
	}
	if want := fmt.Sprintf(`data-restored-count="%d"`, len(recovered)); !strings.Contains(after.body, want) {
		t.Errorf("R-23: the rebuilt Pad's sheet does not carry %s — the count line must say how many names are second-hand", want)
	}

	// The crew that was on the road when the Pad died arrives at the
	// REBUILT post: this incarnation observes them, so they are not tagged
	// — and the recovered names still are.
	if _, err := domain.RecordExpectedArrival(c.b.st, "site-b", c.b.epoch(), c.rollingTrip.TripID, nil, 0, "default"); err != nil {
		t.Fatalf("the rebuilt Pad could not record TRK-200's arrival: %v", err)
	}
	final := renderMuster(t, c.b)
	if got := final.tally(t, "on-site"); got != len(recovered)+len(c.stillRolling) {
		t.Fatalf("after TRK-200 arrives the rebuilt Pad reads %d on site, want %d", got, len(recovered)+len(c.stillRolling))
	}
	if tagged := restoredTagged(t, final); !sortedEqual(tagged, recovered) {
		t.Errorf("after a first-hand arrival the RESTORED set is %v, want exactly the recovered names %v — an arrival this post saw itself must not be tagged, and a recovered name must not lose its tag", tagged, recovered)
	}
	sec := final.section(t, "on-site")
	for _, name := range names(c.stillRolling) {
		if !strings.Contains(sec, "<li>"+name+"</li>") {
			t.Errorf("TRK-200's %q — checked in by THIS incarnation of the Pad — is not listed as a plain, first-hand name", name)
		}
	}

	// The gate roster says the same thing about the same rows.
	rec := httptest.NewRecorder()
	c.b.web.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/gate", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /gate on the rebuilt Pad: %d", rec.Code)
	}
	gate := rec.Body.String()
	if want := fmt.Sprintf(`data-roster-restored="%d"`, len(recovered)); !strings.Contains(gate, want) {
		t.Errorf("/gate on the rebuilt Pad does not carry %s", want)
	}
	if got := strings.Count(gate, `data-restored-visit="`); got != len(recovered) {
		t.Errorf("/gate on the rebuilt Pad tags %d rows RESTORED, want %d (the recovered names, not TRK-200's crew)", got, len(recovered))
	}

	// And the two posts still agree on the record itself: the marking is a
	// READ against this post's identity, nothing new replicates.
	settle(t, c.a, c.b)
	if got, want := string(mustDumpOf(t, c.b.st)), string(mustDumpOf(t, c.a.st)); got != want {
		t.Fatal("the rebuilt Pad and Ice Road do not agree after the rebuild — the marker must not have become a replicated fact")
	}
	_ = context.Background
}
