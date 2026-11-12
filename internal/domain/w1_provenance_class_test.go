// w1_provenance_class_test.go — W1-9: "The ill state produced once, against
// the named classification helper (TP §7.8 as rewritten in round 2): a
// stored `default` row with OccurredAt != AuthoredAt, and a row with an
// unknown time_source; domain.ProvenanceClass classifies both
// UntrustedProvenance, never Default — classification asserted at rest; the
// render half is TP's header debt 2, owed at the first consumer build,
// never here." (WINDOW-BUILD-PLAN-v1 §3 W1, LOCKED 2026-08-27.)
//
// ⚠ COMPILE-BLOCKED PENDING BUILD: domain.ProvenanceClass is a W1
// DELIVERABLE (the at-rest classification helper every later consumer is
// obliged to classify through — NOT W2's §7.4 display-matrix helper, NOT a
// renderer). TP §7.8: the name is indicative, the contract binding. This
// file binds the indicative spelling
//
//	ProvenanceClass(timeSource string, occurredAtMS, authoredAtMS int64)
//
// returning a value comparable against ProvenanceDefault / ProvenanceEntered
// / UntrustedProvenance (the criterion's own class names). A differing
// spelling that keeps the contract goes to the gate, not into a test edit by
// the builder.
//
// KILLER MUTANTS: m10 — ProvenanceClass returns Default on the ill pair
// state or an unknown time_source; m3 — write-side normalisation erases the
// ill state before the helper ever sees it (the at-rest half below is what
// catches m3: the ill rows are STORED, then read back, then classified).
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

// The helper's contract, at the unit level: fail CLOSED at read (TP §4.1
// rule 2), in ONE place, never inline per surface.
func TestW1ProvenanceClassFailsClosedOnBothIllStates(t *testing.T) {
	// Ill state 1: a `default` claim whose pair equality is broken — the
	// rule-W invariant does not hold, so "observation" would be a lie.
	if got := ProvenanceClass("default", 111, 222); got != UntrustedProvenance {
		t.Fatalf("W1-9: default with OccurredAt(111) != AuthoredAt(222) classified %v, want UntrustedProvenance (mutant m10)", got)
	}
	// Ill state 2: an unknown time_source — including the post-bump third
	// value an older binary must render as untrusted rather than break on.
	if got := ProvenanceClass("sundial", 500, 500); got != UntrustedProvenance {
		t.Fatalf("W1-9: unknown time_source %q classified %v, want UntrustedProvenance — never Default (mutant m10)", "sundial", got)
	}

	// Positive controls (a helper that constantly answers Untrusted would
	// pass both ill assertions — the well-formed states prove the channel
	// discriminates):
	if got := ProvenanceClass("default", 500, 500); got != ProvenanceDefault {
		t.Fatalf("W1-9 control: well-formed default classified %v, want ProvenanceDefault", got)
	}
	if got := ProvenanceClass("entered", 111, 222); got != ProvenanceEntered {
		t.Fatalf("W1-9 control: well-formed entered classified %v, want ProvenanceEntered", got)
	}
}

// The ill states PRODUCED AT REST, once (TP §4.1 rule 4): stored through the
// real apply path, read back off the stored rows, and only then classified.
// This is the half that kills m3 — a write-side normalisation (the
// eventtime.go writeEventTime pattern, explicitly forbidden here) would
// erase the ill state between Apply and Events, and the classifications
// below would come back Default/Entered instead of UntrustedProvenance.
func TestW1StoredIllStatesClassifyUntrustedAtRest(t *testing.T) {
	st := newDomainTestStore(t)

	ill1 := store.Envelope{
		SiteID: "site-ill", NodeEpoch: 1, Seq: 1,
		SchemaVersion: store.SchemaVersion, OriginAuth: "site-ill",
		Payload: mustJSON(t, CompanyCreated{Type: "company_created", CompanyID: "site-ill-company-1", Name: "Ill One"}),
	}
	ill1.AuthoredAt.WallMS = 1_700_000_000_000
	ill1.AuthoredAt.MaxErrorUS = 10
	ill1.AuthoredAt.State = store.ClockSynced
	ill1.OccurredAt = 1_699_999_999_000 // != AuthoredAt.WallMS
	ill1.TimeSource = "default"         // the broken-invariant claim

	ill2 := store.Envelope{
		SiteID: "site-ill", NodeEpoch: 1, Seq: 2,
		SchemaVersion: store.SchemaVersion, OriginAuth: "site-ill",
		Payload: mustJSON(t, CompanyCreated{Type: "company_created", CompanyID: "site-ill-company-2", Name: "Ill Two"}),
	}
	ill2.AuthoredAt.WallMS = 1_700_000_000_500
	ill2.AuthoredAt.MaxErrorUS = 10
	ill2.AuthoredAt.State = store.ClockSynced
	ill2.OccurredAt = 1_700_000_000_500
	ill2.TimeSource = "sundial" // unknown source

	for _, env := range []store.Envelope{ill1, ill2} {
		if applied, err := st.Apply(env); err != nil {
			t.Fatalf("W1-9: the ill state must be STORABLE (bytes as sent; refusing it would hide the failure the classification exists to expose): %v", err)
		} else if !applied {
			t.Fatalf("apply %v: deduped", env.Key())
		}
	}

	evs, err := st.Events("site-ill", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 {
		t.Fatalf("want 2 stored ill rows, got %d", len(evs))
	}
	// m3 guard: the stored bytes still carry the ill state.
	if evs[0].TimeSource != "default" || evs[0].OccurredAt == evs[0].AuthoredAt.WallMS {
		t.Fatalf("W1-9/m3: ill pair-state was normalised at write (stored source=%q occ=%d auth=%d) — normalise at display, never at write", evs[0].TimeSource, evs[0].OccurredAt, evs[0].AuthoredAt.WallMS)
	}
	if evs[1].TimeSource != "sundial" {
		t.Fatalf("W1-9/m3: unknown time_source was normalised at write (stored %q)", evs[1].TimeSource)
	}
	// The classification, off the stored rows, through THE one helper.
	if got := ProvenanceClass(evs[0].TimeSource, evs[0].OccurredAt, evs[0].AuthoredAt.WallMS); got != UntrustedProvenance {
		t.Fatalf("W1-9: stored default row with OccurredAt != AuthoredAt classified %v, want UntrustedProvenance (mutant m10)", got)
	}
	if got := ProvenanceClass(evs[1].TimeSource, evs[1].OccurredAt, evs[1].AuthoredAt.WallMS); got != UntrustedProvenance {
		t.Fatalf("W1-9: stored unknown-time_source row classified %v, want UntrustedProvenance (mutant m10)", got)
	}
}
