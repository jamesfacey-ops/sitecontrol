// witness_u8_post19_test.go — witness phase, u8-post19 assignment
// (reports/witness/u8-post19.md). Kills two of the post-19eafc4 census
// survivors on the unsolicited-arrival path (reports/witness/u2-census-rerun.md,
// post19_unreached.tsv), both in RecordUnsolicitedArrival:
//
//	internal/domain/tripauthor.go:747-750  `if rerr != nil {`
//	internal/domain/tripauthor.go:751-753  `if vehicleID = strings.TrimSpace(id); vehicleID == "" {`
//
// Both fire only on the deferred-resolver call shape W7-4(d)(i) added
// (a func, not a typed number) — no test called RecordUnsolicitedArrival
// with a resolver that actually returns a distinguishable outcome before
// this file. See internal/web/witness_u8_post19_test.go for this path's
// third unreached block, one level up in the HTTP handler.
package domain

import (
	"errors"
	"testing"
)

// TestWitnessU8RecordUnsolicitedArrivalWrapsResolverRefusalPreservingIdentity
// kills tripauthor.go:747-750. When the deferred vehicle resolver itself
// refuses, this is the ONE place that error crosses into the door's own
// "domain: unsolicited arrival: %w" convention.
//
// ⛔ The assertion is on IDENTITY, not text containment. A plant that
// re-authors a fresh error with similar wording instead of wrapping with
// %w — fmt.Errorf("domain: unsolicited arrival: "+rerr.Error()), say — would
// still print a message a strings.Contains check accepts; errors.Is against
// the resolver's own sentinel does not.
func TestWitnessU8RecordUnsolicitedArrivalWrapsResolverRefusalPreservingIdentity(t *testing.T) {
	st := newDomainTestStore(t)
	driver, err := CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatalf("create driver: %v", err)
	}

	sentinel := errors.New("vehicle: refused by resolver (test sentinel)")
	resolver := func() (string, error) { return "", sentinel }

	before, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}

	_, err = RecordUnsolicitedArrival(st, "site-a", 1, "", "site-b",
		[]string{driver.PersonID}, nil, 0, "default", resolver)
	if err == nil {
		t.Fatal("a refusing resolver must refuse the arrival, got nil error")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %q does not wrap the resolver's own sentinel via errors.Is — "+
			"tripauthor.go:747-750 must wrap with %%w, not re-author a fresh string", err)
	}
	const wantPrefix = "domain: unsolicited arrival: "
	if got := err.Error(); len(got) < len(wantPrefix) || got[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("error = %q, want the door's own %q convention preserved", got, wantPrefix)
	}

	after, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("a refused arrival authored %d event(s), want 0 (watermark %d -> %d)", after-before, before, after)
	}
}

// TestWitnessU8RecordUnsolicitedArrivalRefusesWhenResolverYieldsNoID kills
// tripauthor.go:751-753 — the resolver's OTHER failure shape: no error, but
// no usable id either (the same shape vehicleIDForTypedNumber's own doc
// calls out: "an EMPTY id and NO refusal for an empty box"). This is a
// DIFFERENT refusal from the one above — the door's own "vehicle id is
// required" copy, not whatever the resolver said, because the resolver said
// nothing at all.
//
// The resolver returns whitespace, not "", so the assertion exercises the
// `strings.TrimSpace(id)` inside this exact block rather than just the
// `id == ""` a resolver-returns-literally-empty fixture would already hit
// on its own.
func TestWitnessU8RecordUnsolicitedArrivalRefusesWhenResolverYieldsNoID(t *testing.T) {
	st := newDomainTestStore(t)
	driver, err := CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatalf("create driver: %v", err)
	}

	resolver := func() (string, error) { return "   ", nil }

	before, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}

	_, err = RecordUnsolicitedArrival(st, "site-a", 1, "", "site-b",
		[]string{driver.PersonID}, nil, 0, "default", resolver)
	if err == nil {
		t.Fatal("a resolver yielding no usable id must refuse the arrival, got nil error")
	}
	const want = "domain: unsolicited arrival: vehicle id is required (it is the correlation key, §9.2)"
	if got := err.Error(); got != want {
		t.Fatalf("error = %q, want exactly %q — a resolver that returns nil error and no usable id must "+
			"produce the door's OWN required-field refusal, not the resolver's (it said nothing)", got, want)
	}

	after, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("a refused arrival authored %d event(s), want 0 (watermark %d -> %d)", after-before, before, after)
	}
}
