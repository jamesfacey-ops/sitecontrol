// w4_web_seams_test.go — THE ONE place W4's web tests bind every
// HTTP/rendering spelling the LOCKED criteria leave undetermined
// (design/WINDOW-BUILD-PLAN-v1.md "### W4", criteria LOCKED 2026-08-27;
// plan §5 step 2), plus the shared fixture worlds. No assertions of its
// own.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD (domain.CorrectOccurredAt,
// domain.TimeCorrection, domain.ResolveOccurredAt, domain.ResolvedTime) —
// that IS the web suite's clean-tree red, captured via go vet.
//
// SHAPE PRESUMPTIONS (SEAMS), each bound once; disputes go to the gate:
//
//   - SEAM W4-W-ROUTE: the correction-authoring surface (TP §4.8 row 14,
//     the named fourteenth surface) is POST /time/correct, behind login
//     like every authoring route; its PRG confirmation is 303 → the
//     correction surface with ?recorded=<site_id>.<epoch>.<seq> (VF §5 —
//     the parameter name and right-anchored parse are DOC-fixed, the route
//     path is the seam). Tests assert against the page the 303 lands on.
//   - SEAM W4-W-FORM: form fields are
//     target                = <site_id>.<epoch>.<seq> (the ?recorded=
//     encoding, right-anchored parse)
//     corrected_occurred_at = the datetime-local wall string (W2's
//     SEAM W2-S-B round trip w2Wall/w2ParseWall),
//     named after the pinned payload literal
//     recorded_by           = the officer attribution
//     There is NO time_source field on this form: the typed instant is
//     PAYLOAD, and overriding the correction act's own time is DECLINED
//     UI (D-44; VF §5; TP §4.8 row 14's exempt classification) — the
//     bounds fire on the payload instant unconditionally.
//   - SEAM W4-W-RENDER: rendered-outcome phrases are asserted by VF §5's
//     own load-bearing words, case-insensitively, over markup with
//     style/script stripped (W3's SEAM W3-W-RENDER discipline; the
//     adverse register and rendering-4 phrase are SHARED with W3 —
//     w3AdverseTokens / w3Rendering4Phrase):
//     losing confirmation  = contains "superseded" (rendering 3's own
//     verb-form, an adverse token) AND the winning
//     post's config-derived display name AND the
//     recovery phrase "radio it through"
//     winning confirmation = contains "supersedes" (rendering 2's
//     verb-form — NOT a substring of any adverse
//     token, so a T5 asserting it plus
//     adverse-absence is m7′-capable, which a
//     grep-for-"supersedes"-alone test is not) AND
//     the SUPERSEDED post's display name AND — the
//     officer suffix, correction carrier only,
//     gated on non-empty recorded_by — the losing
//     officer's name; the adverse register is
//     ABSENT; "effective" present
//   - SEAM W4-W-CFG: reuses W3's fixture configs (w3ConfigSiteA/B —
//     site-a "Ice Road" / site-b "Pad Site Control", mirroring the deploy
//     TOMLs); every EXPECTED post name is DERIVED from the config the
//     server loads via w4DisplayName below, never hardcoded.
package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

const (
	w4CorrectPath      = "/time/correct"         // SEAM W4-W-ROUTE
	w4FieldTarget      = "target"                // SEAM W4-W-FORM
	w4FieldCorrectedAt = "corrected_occurred_at" // SEAM W4-W-FORM (= the pinned payload literal)
	w4FieldRecordedBy  = "recorded_by"           // SEAM W4-W-FORM
	w4SupersedesPhrase = "supersedes"            // SEAM W4-W-RENDER (VF §5 rendering 2)
	w4RecoveryPhrase   = "radio it through"      // SEAM W4-W-RENDER (VF §5 rendering 3's recovery sentence)
	w4EvtCorrected     = "occurred_at_corrected" // DOC — TP §4.7
)

// w4TargetParam renders a key in the ?recorded=/target encoding.
func w4TargetParam(k store.Key) string {
	return fmt.Sprintf("%s.%d.%d", k.SiteID, k.NodeEpoch, k.Seq)
}

// w4DisplayName derives a site's officer-facing name from the config the
// server actually loads (SEAM W4-W-CFG — never hardcoded in a test body).
func w4DisplayName(t *testing.T, cfg *config.Config, siteID string) string {
	t.Helper()
	if cfg.SiteID == siteID {
		return cfg.DisplayName
	}
	for _, d := range cfg.Destinations {
		if d.ID == siteID {
			return d.DisplayName
		}
	}
	t.Fatalf("fixture defect: config for %s derives no display name for %s", cfg.SiteID, siteID)
	return ""
}

// w4CorrectionForm builds the correction POST body (SEAM W4-W-FORM).
func w4CorrectionForm(target store.Key, wall, recordedBy string) url.Values {
	form := url.Values{}
	form.Set(w4FieldTarget, w4TargetParam(target))
	form.Set(w4FieldCorrectedAt, wall)
	form.Set(w4FieldRecordedBy, recordedBy)
	return form
}

// w4PostCorrection posts a correction with a kept session.
func w4PostCorrection(t *testing.T, srv *Server, form url.Values) (*httptest.ResponseRecorder, *http.Cookie) {
	t.Helper()
	return w2PostForm(t, srv, w4CorrectPath, form)
}

// w4CheckedOutWorld builds the correction fixture the LOCKED criteria
// name (TP §7.10's bounds bullet; W4-4): a site-a post where one person
// has been checked in and then checked OUT through the real HTTP surface
// with an ENTERED (mistyped) checkout time — returning the server, store,
// the visit_checked_out envelope (the correction target), and the fixture
// persons. rosterClockMS positions the records floor (newW2Server's own
// contract).
func w4CheckedOutWorld(t *testing.T, rosterClockMS, mistypedMS int64) (*Server, *store.Store, store.Envelope, []domain.Person) {
	t.Helper()
	srv, st, persons := newW2Server(t, rosterClockMS)
	// ⟦FIX17b, sc-stress F12⟧ the check-in is at w2Base−2h+5m, BEFORE every
	// mistyped check-out time this fixture is given (w2Base−2h+10m): the gate
	// now refuses a typed check-out earlier than the check-in, so the
	// mistyped time is wrong but possible. ⟦was: w2Base−w2Hour — after the
	// mistyped check-out, which made the fixture a visit of −50 minutes⟧
	st.Clock = w2Reading(w2Base-2*w2Hour+5*w2Minute, store.ClockSynced)
	w2CheckIn(t, st, persons[:2])
	st.Clock = w2Reading(w2Base, store.ClockSynced)

	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatalf("fixture: %d open visits, want 2", len(open))
	}
	// ⟦PLAN OWNER 2026-08-29 — NONDETERMINISM repair. This read
	// `open[0].VisitID`, which is a COIN FLIP: newID mints visit ids from
	// crypto/rand (`internal/domain/ids.go`) and OpenVisitsAt returns them
	// content-keyed by visit_id, so which of the two checked-in persons sorts
	// first is random. W4-4's probe asserts the DEPARTED person (persons[0],
	// Dana Driver) can no longer be checked out — true only when the flip
	// landed that way. Measured by the W4 builder: 7 failures in 18 runs,
	// ~40%. Selecting by PersonID makes the fixture's own stated premise —
	// "the driver of the checked-out visit" — true by construction instead of
	// by chance. No assertion, key, clock or criterion changes.
	//
	// The production alternative was refused deliberately: ordering
	// OpenVisitsAt "driver first" would invent semantics to fit a fixture, and
	// the visit dump order must stay content-keyed or the permuted-order
	// determinism tests break.⟧
	var departed domain.Visit
	for _, v := range open {
		if v.PersonID == persons[0].PersonID {
			departed = v
		}
	}
	if departed.VisitID == "" {
		t.Fatalf("fixture: no open visit for %s (%s) among %d open — the premise of this fixture is that persons[0] is the one checked out", persons[0].PersonID, persons[0].Name, len(open))
	}

	before := w2Watermark(t, st, "site-a", 1)
	form := url.Values{}
	form.Add("visit_ids", departed.VisitID)
	form.Set(w2TimeSourceField, "entered")
	form.Set(w2OccurredAtField, w2Wall(mistypedMS))
	rec, _ := w2PostForm(t, srv, "/gate/checkout", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("fixture checkout POST = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	outs := w2EventsOfType(t, w2EventsFrom(t, st, "site-a", 1, before), "visit_checked_out")
	if len(outs) != 1 {
		t.Fatalf("fixture: %d visit_checked_out events minted, want 1", len(outs))
	}
	if outs[0].TimeSource != "entered" || outs[0].OccurredAt != mistypedMS {
		t.Fatalf("fixture: checkout stored (%q, %d), want (\"entered\", %d) — W2's capture must hold before a correction can target it", outs[0].TimeSource, outs[0].OccurredAt, mistypedMS)
	}
	return srv, st, outs[0], persons
}

// w4RemoteCorrection authors ONE correction as siteID on a scratch post
// primed with the given history, at the given clock, and returns its
// envelope — wire-shaped output of a real authoring edge at the other
// post (w3RemoteAmend's twin, through SEAM W4-S-EDGE).
func w4RemoteCorrection(t *testing.T, primer []store.Envelope, siteID string, clockMS int64, target store.Key, correctedMS int64, recordedBy string) store.Envelope {
	t.Helper()
	scratch := w3OpenRegistered(t, "w4-scratch-"+siteID+".db")
	w3ApplyWire(t, scratch, primer)
	scratch.Clock = w2Reading(clockMS, store.ClockSynced)
	env, err := domain.CorrectOccurredAt(scratch, siteID, 1, domain.TimeCorrection{
		Target: target, CorrectedOccurredAtMS: correctedMS, RecordedBy: recordedBy,
	})
	if err != nil {
		t.Fatalf("remote fixture correction refused: %v", err)
	}
	return env
}

// w4MintedCorrection returns the ONE occurred_at_corrected event a POST
// minted on the (site, epoch) stream after `before`, fatally otherwise.
func w4MintedCorrection(t *testing.T, st *store.Store, siteID string, epoch, before int64) store.Envelope {
	t.Helper()
	minted := w2EventsFrom(t, st, siteID, epoch, before)
	corrs := w2EventsOfType(t, minted, w4EvtCorrected)
	if len(corrs) != 1 {
		t.Fatalf("the correction POST minted %d %s event(s), want exactly 1 (minted: %d events total)", len(corrs), w4EvtCorrected, len(minted))
	}
	return corrs[0]
}

// w4PayloadCorrectedMS extracts the pinned corrected_occurred_at payload
// field from a stored correction envelope.
func w4PayloadCorrectedMS(t *testing.T, env store.Envelope) int64 {
	t.Helper()
	var pl struct {
		CorrectedAt *int64 `json:"corrected_occurred_at"`
	}
	if err := json.Unmarshal(env.Payload, &pl); err != nil {
		t.Fatalf("correction payload: %v", err)
	}
	if pl.CorrectedAt == nil {
		t.Fatalf("correction payload carries no corrected_occurred_at: %s", env.Payload)
	}
	return *pl.CorrectedAt
}

// w4FollowRecorded asserts the PRG shape and returns the confirmation
// page the officer reads plus the recorded key (w3FollowRecorded +
// w3RecordedKey, composed).
func w4FollowRecorded(t *testing.T, srv *Server, rec *httptest.ResponseRecorder, cookie *http.Cookie) (page, location string, recorded store.Key) {
	t.Helper()
	page, location = w3FollowRecorded(t, srv, rec, cookie)
	return page, location, w3RecordedKey(t, location)
}

// w4ResolveBudgetedWeb is the web-package twin of the domain harness's
// budgeted resolve (SEAM W4-S-BUDGET's form, reused so no web assertion
// can hang the suite either).
func w4ResolveBudgetedWeb(t *testing.T, st *store.Store, target store.Key) (domain.ResolvedTime, []domain.Correction, error) {
	t.Helper()
	type result struct {
		eff   domain.ResolvedTime
		chain []domain.Correction
		err   error
	}
	done := make(chan result, 1)
	go func() {
		eff, chain, err := domain.ResolveOccurredAt(st, target)
		done <- result{eff, chain, err}
	}()
	for i := 0; i < 600; i++ {
		select {
		case r := <-done:
			return r.eff, r.chain, r.err
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	t.Fatalf("step budget EXHAUSTED in ResolveOccurredAt(%v) — the resolver did not terminate (mTP-7b's shape); this red is the named test's own", target)
	return domain.ResolvedTime{}, nil, nil
}
