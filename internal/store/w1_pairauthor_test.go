// w1_pairauthor_test.go — THE ONE place W1's store tests bind the shape of
// the post-rebase authoring call and the v10 envelope fixture.
// (WINDOW-BUILD-PLAN-v1 §3 W1, LOCKED 2026-08-27.)
//
// ⚠ COMPILE-BLOCKED PENDING BUILD: this file cannot compile until W1's
// production change lands (Envelope.AuthoredAt{WallMS,MaxErrorUS,State},
// Envelope.OccurredAt, Envelope.TimeSource, and Store.Author gaining the
// pair). That failing compile IS the ledger red for the new-field tests.
//
// SHAPE PRESUMPTION, stated for the gate (§5 step 3): the locked criteria
// fix WHAT is passed (the OccurredAt/TimeSource pair; rule W applied inside
// Author from ONE clock reading) but not the exact Go spelling. These
// helpers presume `Author(siteID, nodeEpoch, payload, occurredAtMS int64,
// timeSource string)` — the minimal non-backward-compatible reading of
// "Store.Author … gains the pair". If the builder's chosen spelling differs
// while satisfying W1-10's census-shape test, the dispute goes to the gate;
// every assertion in the files that use these helpers stands regardless of
// the spelling.
package store

import (
	"encoding/json"
	"testing"
)

// w1Reading builds a fixed audit-clock reading (the store suite's fixedClock
// lives in eventtime_test.go, which W1 deletes with its subject — W1 tests
// carry their own).
func w1Reading(wallMS, maxErrUS int64, state ClockState) Clock {
	return func() ClockReading {
		return ClockReading{WallMS: wallMS, MaxErrorUS: maxErrUS, State: state}
	}
}

// w1Author authors one event with the OccurredAt/TimeSource pair.
// timeSource "default" takes rule W's path (the server copies OccurredAt
// from the single AuthoredAt clock reading; occurredAtMS is the caller's
// echo and must be ignored on that path); "entered" stores occurredAtMS
// byte-exactly.
func w1Author(t *testing.T, s *Store, siteID string, nodeEpoch int64, payload json.RawMessage, occurredAtMS int64, timeSource string) Envelope {
	t.Helper()
	env, err := s.Author(siteID, nodeEpoch, payload, occurredAtMS, timeSource)
	if err != nil {
		t.Fatalf("author (%s, %d, occurred=%d, source=%q): %v", siteID, nodeEpoch, occurredAtMS, timeSource, err)
	}
	return env
}

// w1Envelope builds a v10 envelope with the five new values set — the
// peer-injection fixture shape (Apply does not care whose site_id the
// envelope bears; injecting foreign-site events is the sanctioned test
// mechanism, store/log.go). AuthoredAt.State is typed here as ClockState
// (clock.go's existing three-state vocabulary, which B-2 freezes into the
// hash at W1); A §6's sketch writes `State string` — if the builder keeps a
// bare string, only this constructor's one conversion changes.
func w1Envelope(site string, epoch, seq int64, payload json.RawMessage, authWallMS, authMaxErrUS int64, authState ClockState, occurredAtMS int64, timeSource string) Envelope {
	env := Envelope{
		SiteID:        site,
		NodeEpoch:     epoch,
		Seq:           seq,
		SchemaVersion: SchemaVersion,
		OriginAuth:    site,
		Payload:       payload,
	}
	env.AuthoredAt.WallMS = authWallMS
	env.AuthoredAt.MaxErrorUS = authMaxErrUS
	env.AuthoredAt.State = authState
	env.OccurredAt = occurredAtMS
	env.TimeSource = timeSource
	return env
}
