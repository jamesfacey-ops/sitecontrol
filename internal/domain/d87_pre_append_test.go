// d87_pre_append_test.go — D-87's refusal is PRE-APPEND, witnessed by DIGEST
// rather than by watermark alone.
//
// The four refusal tests in d87_choke_point_test.go assert the watermark
// and the own stream's length. Both are blind to the failure that matters
// most in an event-sourced post: a PROJECTION row changing under a refusal
// while no event records it — a phantom write a rebuild from the log would
// not reproduce, so the two posts drift with identical logs. The tester for
// D-82 measured that a watermark-only assertion needed a second mutation to
// prove it had teeth; this file carries both fingerprints — the event log
// (every stream, by Envelope.Hash) and every registered projection
// (DumpProjections, canonical JSON) — and its teeth were produced against
// the "run the folds, commit, then refuse" mutant, which the watermark and
// stream-length assertions pass and only the projection digest catches
// (reports/witness/TESTER-d87.md §2).
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"testing"

	"sitecontrol/internal/store"
)

// d87Digest is two fingerprints of one post: Log covers every envelope of
// every (site, epoch) stream named, in (site, epoch, seq) order — sorted
// explicitly, never by map order (C1); Projections is DumpProjections'
// canonical JSON, which is already keyed and ordered by the store.
type d87Digest struct {
	Log, Projections string
	Envelopes        int
}

func d87DigestOf(t *testing.T, st *store.Store, sites ...string) d87Digest {
	t.Helper()
	sites = append([]string(nil), sites...)
	sort.Strings(sites)
	h := sha256.New()
	n := 0
	for _, site := range sites {
		epochs, err := st.Epochs(site)
		if err != nil {
			t.Fatal(err)
		}
		sort.Slice(epochs, func(i, j int) bool { return epochs[i] < epochs[j] })
		for _, ep := range epochs {
			envs, err := st.Events(site, ep, 1, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range envs {
				h.Write([]byte(e.Hash()))
				h.Write([]byte{0})
				n++
			}
		}
	}
	dump, err := st.DumpProjections()
	if err != nil {
		t.Fatal(err)
	}
	p := sha256.Sum256(dump)
	return d87Digest{Log: hex.EncodeToString(h.Sum(nil)), Projections: hex.EncodeToString(p[:]), Envelopes: n}
}

// TestD87ARefusalLeavesTheLogAndEveryProjectionByteIdentical: for each of
// the four gated types, camp-7's refused write against camp-9's row leaves
// camp-7's post byte-identical — the whole event log (both sites' streams,
// by hash), every projection table, and the watermark. Then the INSTRUMENT
// CONTROL: an accepted write (a shared-reference amendment, D-89) moves both
// digests, so an equal-before-and-after is a measurement and not a constant.
func TestD87ARefusalLeavesTheLogAndEveryProjectionByteIdentical(t *testing.T) {
	p := d87TwoPosts(t)
	sites := []string{"camp-7", "camp-9"}
	for _, g := range d87GatedPayloads(t, p) {
		t.Run(g.typ, func(t *testing.T) {
			before := d87DigestOf(t, p.st7, sites...)
			if before.Envelopes == 0 {
				t.Fatal("PRECONDITION: camp-7 holds no envelopes; the fixture's replication did not land")
			}
			wmBefore := d87Watermark(t, p.st7, "camp-7")

			_, err := p.st7.Author("camp-7", 1, g.payload, 0, "default")
			var own *store.OwnershipError
			if !errors.As(err, &own) {
				t.Fatalf("PRECONDITION: %s was not refused with *store.OwnershipError (err=%v %T); the digest below would measure an accepted write", g.typ, err, err)
			}

			after := d87DigestOf(t, p.st7, sites...)
			if after.Log != before.Log {
				t.Errorf("%s: the EVENT LOG changed under a refusal (%d → %d envelopes; digest %.12s → %.12s) — a refusal is PRE-APPEND",
					g.typ, before.Envelopes, after.Envelopes, before.Log, after.Log)
			}
			if after.Projections != before.Projections {
				t.Errorf("%s: a PROJECTION changed under a refusal (digest %.12s → %.12s) — a phantom write no event records; a rebuild from the log would not reproduce this post",
					g.typ, before.Projections, after.Projections)
			}
			if wm := d87Watermark(t, p.st7, "camp-7"); wm != wmBefore {
				t.Errorf("%s: camp-7's watermark moved %d → %d under a refusal", g.typ, wmBefore, wm)
			}
		})
	}

	// INSTRUMENT CONTROL — the digest MOVES under an accepted write. camp-7
	// correcting a camp-9-enrolled person is shared reference data (D-89)
	// and must land: one envelope appended, one side-table row folded.
	before := d87DigestOf(t, p.st7, sites...)
	phone := "555-0187"
	if _, err := AmendPerson(p.st7, "camp-7", 1, PersonAmendment{PersonID: p.personID, Phone: &phone}, 0, "default"); err != nil {
		t.Fatalf("CONTROL: the shared-reference amendment was refused: %v", err)
	}
	after := d87DigestOf(t, p.st7, sites...)
	if after.Log == before.Log || after.Envelopes != before.Envelopes+1 {
		t.Fatalf("CONTROL FAILED: the log digest did not move under an accepted write (%d → %d envelopes) — the instrument cannot see appends", before.Envelopes, after.Envelopes)
	}
	if after.Projections == before.Projections {
		t.Fatalf("CONTROL FAILED: the projection digest did not move under an accepted write — the instrument cannot see folds")
	}
}
