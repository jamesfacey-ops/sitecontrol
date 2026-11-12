// g13_wiring_census_test.go — G13 coverage close (tester report,
// 2026-09-08): "a bare store is inert, no production path reaches
// store.Author unregistered" was MEASURED once — cmd/sitectl calls
// domain.Register unconditionally; cmd/dumpcompare opens a store (to dump
// its registered projection names, ⟦LOCK — §67⟧) but never authors — and
// then repeated as a fact by everyone downstream of that one pass. This is
// the executable version: it fails the day a THIRD binary opens a store
// without registering first, or an edit drops the existing domain.Register
// call from either of the two that exist today.
//
// Reuses d87_choke_point_test.go's non-test source walker (same package);
// no new extraction machinery for a two-substring, two-file check.
package domain

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestG13EveryStoreOpenInNonTestSourceIsFollowedByDomainRegister(t *testing.T) {
	// The whole module root, not just cmd/ — the property is "nowhere",
	// not "nowhere I already knew to look".
	sources := d87NonTestGoSources(t, filepath.Join("..", ".."))
	if len(sources) == 0 {
		t.Fatal("PRECONDITION: the walk found no non-test source at all — wrong root, this census would silently police nothing")
	}
	checked := 0
	for path, src := range sources {
		openAt := strings.Index(src, "store.Open(")
		if openAt < 0 {
			continue // this file never opens a store; not this census's concern
		}
		checked++
		registerAt := strings.Index(src, "domain.Register(")
		if registerAt < 0 {
			t.Errorf("%s calls store.Open but never domain.Register in the same file — G13's fail-closed check would never arm here, and every Author reachable from this file would run against a bare (inert) store", path)
			continue
		}
		if registerAt < openAt {
			t.Errorf("%s calls domain.Register before store.Open — there is no store yet to register against", path)
		}
	}
	if checked == 0 {
		t.Fatal("PRECONDITION: no non-test source calls store.Open anywhere — the census cannot see what it is meant to police (today: cmd/sitectl, cmd/dumpcompare)")
	}
	if checked != 2 {
		t.Logf("NOTE: %d file(s) call store.Open (was 2: cmd/sitectl, cmd/dumpcompare) — not a failure, just worth knowing the count moved", checked)
	}
}
