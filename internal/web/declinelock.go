package web

// declinelock.go — James, 2026-10-03: duplicate-submit lock covers
// deliberate declines.
//
// W3's gateCommitMu (gate.go, M-1 / G-028) serialises find-then-mint, so a
// second identical check-in of a NEW name finds the person the first one
// minted. A deliberate "No — this is a different person" decline escapes
// that: its row is posted as kind=new and is minted as a namesake BY DESIGN,
// whatever the roster holds — so the same decline sent twice at the same
// moment (a retry, a double send, two tabs) minted two namesakes and checked
// both in, and the muster counted one human twice.
//
// THE RULE. A check-in submission that would mint at least one person is
// fingerprinted (this post, the vehicle, and every row — a known person by
// id, a new one by folded name + company + trade). An IDENTICAL submission
// within duplicateSubmitWindow of a successful one authors NOTHING and lands
// on the first one's outcome: the same 303 to the same confirmation, so it
// resolves to the person the first one minted and adds no second open
// visit. Outside the window, or with any difference (another company, say),
// it is a genuinely new decline and mints as before.
//
// Process memory, deliberately: it guards one process's door against
// near-simultaneous resubmission; nothing replicated or alert-critical lives
// in it, and a restart forgetting it costs only the pre-ruling behaviour.
// Read and written only under gateCommitMu, which is held for the whole
// commit — so the second request's lookup runs after the first one's commit
// and its remembered outcome.

import (
	"sort"
	"strings"
	"time"

	"sitecontrol/internal/domain"
)

// duplicateSubmitWindow is the "short window" of the ruling (James's
// example: 10 s). A double send, a retry after a slow page, a second tab
// all land well inside it; an officer deliberately checking in a second
// namesake re-types the whole form, which takes longer.
const duplicateSubmitWindow = 10 * time.Second

type recentSubmit struct {
	target string
	at     time.Time
}

// checkInFingerprint identifies a check-in submission for the lock; ok is
// false when nothing in it would be minted (no decline, no new name), so
// the lock never touches an ordinary check-in of known people.
func checkInFingerprint(siteID string, f vehicleForm, driver domain.PersonSlot, occupants []domain.PersonSlot) (string, bool) {
	fold := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
	mints := false
	slot := func(p domain.PersonSlot) string {
		if p.ExistingID != "" {
			return "id:" + p.ExistingID
		}
		mints = true
		return "new:" + fold(p.NewName) + "|" + p.CompanyID + "|" + fold(p.NewCompany) + "|" + p.TradeID + "|" + fold(p.NewTrade)
	}
	rows := make([]string, 0, len(occupants))
	for _, o := range occupants {
		rows = append(rows, slot(o))
	}
	sort.Strings(rows)
	vehicle := fold(f.Vehicle)
	if f.VehicleUnknown {
		vehicle = "\x00unknown"
	}
	fp := siteID + "\x1f" + vehicle + "\x1fD:" + slot(driver) + "\x1f" + strings.Join(rows, "\x1e")
	return fp, mints
}

func (s *Server) submitClock() time.Time {
	if s.SubmitNow != nil {
		return s.SubmitNow()
	}
	return time.Now()
}

// duplicateOutcome: the remembered outcome of an identical submission
// inside the window, if any. Caller holds gateCommitMu.
func (s *Server) duplicateOutcome(fp string) (string, bool) {
	now := s.submitClock()
	for k, v := range s.recentSubmits {
		if now.Sub(v.at) > duplicateSubmitWindow || now.Before(v.at.Add(-duplicateSubmitWindow)) {
			delete(s.recentSubmits, k)
		}
	}
	v, ok := s.recentSubmits[fp]
	return v.target, ok
}

// rememberOutcome records a successful minting submission. Caller holds
// gateCommitMu.
func (s *Server) rememberOutcome(fp, target string) {
	if s.recentSubmits == nil {
		s.recentSubmits = map[string]recentSubmit{}
	}
	s.recentSubmits[fp] = recentSubmit{target: target, at: s.submitClock()}
}
