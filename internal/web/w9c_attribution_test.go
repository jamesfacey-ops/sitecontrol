// w9c_attribution_test.go — W9c-10: attribution rendered, THREE halves, and
// the third is rendered on the SAME PAGE as its control.
//
// design/WINDOW-BUILD-PLAN-v1.md "#### W9c", criterion W9c-10 ⟦REWRITTEN at
// the rev-4 fold — W9R2-HR-5 / W9R2-TE-7⟧, against
// design/alert-unack-v1.md revision 4 §5 (R1 and R2) and §7 row T8.
//
// KILLER MUTANTS: m8b — THE AMNESIAC RE-ARM: the rendering requirement
// implemented at rev 3's SCOPE, i.e. attribution and retraction chain on
// ACKED rows only. Halves ⟨a⟩ and ⟨b⟩ both stay green under it, because both
// their fixtures are acked; half ⟨c⟩ goes red, because U's
// returned-to-unacknowledged row renders byte-identical to N's
// never-acknowledged row and post A's officer — who acked ten minutes ago —
// re-acks without ever being shown that a peer repudiated it. m8 — the bare
// suffix: `" (acknowledged)"` unchanged, attribution absent.
//
// ⚠ WORDING IS NOT PINNED, DELIBERATELY. [[D-62]] transfers the question of
// whether W9's screens use one wording style or three to W9's BUILD
// CRITIQUE; a test that froze one sentence here would pre-empt a ruling that
// has an owner and a moment. What is pinned is what the criterion actually
// says is owed: WHO the row names — post names carried as fixture literals —
// and, on the never-acked control, that it names NOBODY.
package web

import (
	"strings"
	"testing"
)

// The two posts' names, as literals. `ackingPost` is this box's configured
// display name (config.PostName, stamped by s.postAttribution on every ack);
// `retractingPost` is carried in the retraction payload's recorded_by, which
// is the only place a peer's attribution can come from before replication.
const (
	w9cAckingPost     = "Ice Camp B"       // post A — the board this page is fetched at
	w9cRetractingPost = "Pad Site Control" // post B — the peer
)

// TestW9c10AttributionAndRepudiationAreRenderedOnTheRow — W9c-10 (T8).
//
// One fixture, one GET, four trips. The halves are asserted with t.Errorf,
// not t.Fatalf, so that every half — including ⟨c⟩'s never-acked control —
// is actually executed on every run rather than hidden behind the first
// failure.
func TestW9c10AttributionAndRepudiationAreRenderedOnTheRow(t *testing.T) {
	srv, st, d, tripA := newAlertingServer(t)
	if srv.Cfg.PostName() != w9cAckingPost {
		t.Fatalf("W9c-10 fixture: this box records as %q, want the stated %q — the acking post's name is a fixture literal", srv.Cfg.PostName(), w9cAckingPost)
	}

	const (
		vehicleA = "ICE TRUCK 7"      // newAlertingServer's own fixture trip
		vehicleB = "TRUCK B REACKED"  // ⟨b⟩ — retract-then-peer-re-ack
		vehicleU = "TRUCK U UNACKED"  // ⟨c⟩ — acked by A, un-acked by B
		vehicleN = "TRUCK N NEVERACK" // ⟨c⟩'s control — never acknowledged
	)
	tripB := dispatchLiveOverdueTrip(t, st, d, vehicleB)
	tripU := dispatchLiveOverdueTrip(t, st, d, vehicleU)
	tripN := dispatchLiveOverdueTrip(t, st, d, vehicleN)

	// ⟨a⟩ an ordinary acked row.
	w9cAckViaHTTP(t, srv, st, tripA.TripID)

	// ⟨b⟩ ack at this post → retract at this post → the PEER re-acks. The
	// effective ack is the peer's, and the repudiation that happened in
	// between was this post's — two DIFFERENT names, so "names the peer's
	// post" and "shows the prior un-acknowledgment" cannot be satisfied by
	// one string.
	ackB := w9cAckViaHTTP(t, srv, st, tripB.TripID)
	w9cRetractAt(t, st, w9cKey{w9cBoardSite, 1, 0}, ackB, w9cAckingPost, 1_700_000_100_000)
	w9cAckAt(t, st, w9cKey{w9cPeerSite, 1, w9cNextSeq(t, st, w9cPeerSite, 1)}, tripB.TripID, w9cRetractingPost, 1_700_000_200_000)

	// ⟨c⟩ U: acked by post A, un-acked by post B. N: never acknowledged.
	ackU := w9cAckViaHTTP(t, srv, st, tripU.TripID)
	w9cRetractAt(t, st, w9cKey{w9cPeerSite, 1, 0}, ackU, w9cRetractingPost, 1_700_000_300_000)

	// --- FIXTURE GUARDS, and they can fail ------------------------------
	board, err := d.Evaluate()
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]struct {
		found  bool
		status string
		acked  bool
	}{}
	for _, a := range board.Alerts {
		state[a.TripID] = struct {
			found  bool
			status string
			acked  bool
		}{true, string(a.Status), a.Acked}
	}
	for _, want := range []struct {
		id, label, status string
		acked             bool
	}{
		{tripB.TripID, "⟨b⟩ the re-acked trip", "overdue_live", true},
		{tripU.TripID, "⟨c⟩ trip U", "overdue_live", false},
		{tripN.TripID, "⟨c⟩ the never-acked control N", "overdue_live", false},
	} {
		got, ok := state[want.id]
		if !ok {
			t.Fatalf("W9c-10 fixture: %s is not on the board at all", want.label)
		}
		// Reported, not fatal: on a tree where the retraction does not
		// resolve, U is still acked — and that IS one of this criterion's
		// reds. Failing here with t.Fatalf would hide the three render
		// halves below, including ⟨c⟩'s control, which must be exercised on
		// every run rather than only once the fold works.
		if got.status != want.status || got.acked != want.acked {
			t.Errorf("W9c-10 fixture: %s is %s/acked=%v, want %s/acked=%v — U and its control must be the SAME status, and U must be currently UN-ACKED and alerting at this post",
				want.label, got.status, got.acked, want.status, want.acked)
		}
	}

	// --- ONE GET. Every half below reads THIS page. ---------------------
	page := getTrips(t, srv)

	rowA := w9cRowText(t, page, "⟨a⟩ the acked trip", vehicleA)
	rowB := w9cRowText(t, page, "⟨b⟩ the re-acked trip", vehicleB)
	rowU := w9cRowText(t, page, "⟨c⟩ trip U", vehicleU)
	rowN := w9cRowText(t, page, "⟨c⟩ the never-acked control N", vehicleN)

	// The regions really are per-trip: a row that swallowed a neighbour
	// would make the control's absence assertion unfalsifiable.
	for _, pair := range []struct {
		label, row string
		others     []string
	}{
		{"⟨c⟩ trip U", rowU, []string{vehicleN, vehicleB, vehicleA}},
		{"⟨c⟩ the never-acked control N", rowN, []string{vehicleU, vehicleB, vehicleA}},
	} {
		for _, other := range pair.others {
			if strings.Contains(pair.row, other) {
				t.Fatalf("W9c-10 fixture: %s's rendered region also contains %q — the per-row extraction has merged two trips, so an absence assertion below would prove nothing", pair.label, other)
			}
		}
	}

	// --- ⟨a⟩ — R1: an acked row names the acking post -------------------
	if !strings.Contains(rowA, w9cAckingPost) {
		t.Errorf("W9c-10 ⟨a⟩: the acked row for %s does not name the acking post %q — today the row carries only the bare %q suffix with no attribution at all (m8, visible-failure attack 1's imperceptible absence). Row:\n%.600s",
			vehicleA, w9cAckingPost, " (acknowledged)", rowA)
	}

	// --- ⟨b⟩ — R1 after retract-then-peer-re-ack ------------------------
	if !strings.Contains(rowB, w9cRetractingPost) {
		t.Errorf("W9c-10 ⟨b⟩: after retract-then-peer-re-ack the row for %s does not name the PEER's post %q, which authored the effective ack. Row:\n%.600s",
			vehicleB, w9cRetractingPost, rowB)
	}
	if !strings.Contains(rowB, w9cAckingPost) {
		t.Errorf("W9c-10 ⟨b⟩: the row for %s does not show the prior un-acknowledgment — the retraction that stands between the two acks was authored by %q and the row names nobody for it. Row:\n%.600s",
			vehicleB, w9cAckingPost, rowB)
	}
	// The urgency word stays in the same breath: this is an ALERTING row
	// that carries an ack (the /trips "OVERDUE by X (acknowledged)" shape),
	// so the attribution must live in the same rendered row as the word
	// OVERDUE, never in some separate panel.
	if !strings.Contains(rowB, "OVERDUE") {
		t.Errorf("W9c-10 ⟨b⟩: the row for %s no longer carries the urgency word OVERDUE — the attribution must ride the same breath as the urgency, not replace it. Row:\n%.600s",
			vehicleB, rowB)
	}

	// --- ⟨c⟩ — R2: the repudiation clause, and its same-page control ----
	if !strings.Contains(rowU, w9cRetractingPost) {
		t.Errorf("W9c-10 ⟨c⟩: trip U is acked by %q and UN-ACKED by %q, and its currently-un-acked alerting row at %q's own store does not name %q — the officer who acknowledged this trip ten minutes ago is shown a row byte-identical to a fresh alert, concludes the ack did not take, and re-acks without ever being told a peer repudiated it (m8b, the amnesiac re-arm). Row:\n%.600s",
			w9cAckingPost, w9cRetractingPost, w9cAckingPost, w9cRetractingPost, rowU)
	}
	if !strings.Contains(rowU, w9cAckingPost) {
		t.Errorf("W9c-10 ⟨c⟩: trip U's row does not name %q, the post whose acknowledgment was repudiated — R2's clause is \"previously acknowledged by ⟨X⟩, un-acknowledged by ⟨Y⟩\" and it owes both. Row:\n%.600s",
			w9cAckingPost, rowU)
	}
	// The control: same page, same GET, same overdue_live status, never
	// acknowledged by anyone — and therefore carrying NO such clause. The
	// control being same-page and same-request by construction, a difference
	// between the two rows cannot be an artifact of two renders.
	for _, name := range []string{w9cAckingPost, w9cRetractingPost} {
		if strings.Contains(rowN, name) {
			t.Errorf("W9c-10 ⟨c⟩ CONTROL: the NEVER-acknowledged row for %s names %q — a never-acked row must carry no acknowledgment and no repudiation clause, or the two states are not distinguishable on the face of the row after all. Row:\n%.600s",
				vehicleN, name, rowN)
		}
	}
}
