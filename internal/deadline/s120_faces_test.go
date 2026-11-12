// s120_faces_test.go — S120: the F6 §5 attribution faces, and the
// duplicate-suppression they ride on.
//
// WHY THIS FILE EXISTS. A mutation audit (S118) made `dedupe` a no-op — the
// helper whose whole job is that "two posts that both acked are two names;
// one post that acked twice is one" — and the WHOLE TREE stayed green:
// `./...` came back byte-identical at PASS=1269 FAIL=25 SKIP=1. A probe on
// the same build proved the path is not merely unreached: dedupe really does
// drop repeats 11 times in one run, including
//
//	"previously acknowledged by Ice Camp B, un-acknowledged by Ice Camp B"
//
// so the surface genuinely produces the duplicates and nothing asserted that
// they are removed. [MEASURED 2026-09-04]
//
// ⚠ AND THE GAP IS WIDER THAN THE MUTANT. `AckFace` had ZERO test references
// anywhere in the tree before this file — R1's attribution string was
// unwitnessed in full, not just its de-duplication. R2 is asserted end-to-end
// by `internal/web`'s W9c-10, but on CONTENT at one post, never on the
// repeat-collapsing this file pins.
//
// WHY A DIRECT TEST AND NOT A FIXTURE. `dedupe`, `ackFace` and
// `repudiationFace` are pure functions of an `ackFaces` value. Driving them
// through a store would add envelope-key coupling that this package's own
// fixtures warn about at length (deadline_test.go:93's routed note: seeding a
// record ahead of a dispatch moves its key and reddens three W9c tests that
// pin keys by hand). The composition FROM the projections is already covered;
// what was missing is what the faces DO with what they are given.
package deadline

import "testing"

// TestF6AckFaceNamesEachPostOnceHoweverManyTimesItAcked is the R1 half.
//
// ⚠ THE TWO ARMS ARE BOTH LOAD-BEARING and neither alone is sufficient:
// without the "two posts" arm a build that returned only the first name would
// pass, and without the "one post twice" arm a build with no de-duplication
// would pass. The mutant this kills is `dedupe` returning its input unchanged.
func TestF6AckFaceNamesEachPostOnceHoweverManyTimesItAcked(t *testing.T) {
	for _, c := range []struct {
		name           string
		acknowledgedBy []string
		want           string
	}{
		{"no effective ack is no face at all", nil, ""},
		{"one post", []string{"Pad Site Control"}, "acknowledged by Pad Site Control"},
		{
			// THE MUTANT'S CASE: one post that acked twice is ONE name.
			"one post that acked twice is one name",
			[]string{"Ice Camp B", "Ice Camp B"},
			"acknowledged by Ice Camp B",
		},
		{
			// The accepting arm: two posts are genuinely two names, so the
			// test is not vacuously satisfied by a build that collapses
			// everything to the first entry.
			"two posts that both acked are two names",
			[]string{"Ice Camp B", "Pad Site Control"},
			"acknowledged by Ice Camp B and Pad Site Control",
		},
		{
			"three posts, one of them twice",
			[]string{"Ice Camp B", "Pad Site Control", "Ice Camp B", "Ridge"},
			"acknowledged by Ice Camp B, Pad Site Control and Ridge",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := &ackFaces{acknowledgedBy: c.acknowledgedBy}
			if got := f.ackFace(); got != c.want {
				t.Fatalf("ackFace() = %q, want %q\n"+
					"F6 §5 R1: the acked row NAMES the post whose acknowledgment stands. "+
					"A post that acked twice is ONE name (unack.go's dedupe): a row reading "+
					"\"acknowledged by X and X\" tells an officer two posts cleared this trip "+
					"when one did, which is the opposite of what the attribution exists to say.",
					got, c.want)
			}
		})
	}
}

// TestF6RepudiationFaceCollapsesAnIdenticalClause is the R2 half.
//
// ⚠ The clause "previously acknowledged by X, un-acknowledged by Y" is built
// per retraction, so the SAME pair can arrive twice — the probe caught exactly
// that in a live run. Two identical sentences joined by "; " reads as two
// separate disputes over one trip.
func TestF6RepudiationFaceCollapsesAnIdenticalClause(t *testing.T) {
	for _, c := range []struct {
		name         string
		repudiations []repudiation
		want         string
	}{
		{"no retraction is no face at all", nil, ""},
		{
			"one retraction",
			[]repudiation{{ackedBy: "Ice Camp B", retractedBy: "Pad Site Control"}},
			"previously acknowledged by Ice Camp B, un-acknowledged by Pad Site Control",
		},
		{
			// THE MUTANT'S CASE, and the exact pair the probe observed.
			"the same pair twice is one clause",
			[]repudiation{
				{ackedBy: "Ice Camp B", retractedBy: "Ice Camp B"},
				{ackedBy: "Ice Camp B", retractedBy: "Ice Camp B"},
			},
			"previously acknowledged by Ice Camp B, un-acknowledged by Ice Camp B",
		},
		{
			// The accepting arm: two DIFFERENT disputes stay two clauses,
			// joined by "; " and never by "and" (unack.go states the reason:
			// each clause already contains a comma).
			"two different disputes stay two clauses",
			[]repudiation{
				{ackedBy: "Ice Camp B", retractedBy: "Pad Site Control"},
				{ackedBy: "Ridge", retractedBy: "Ice Camp B"},
			},
			"previously acknowledged by Ice Camp B, un-acknowledged by Pad Site Control; " +
				"previously acknowledged by Ridge, un-acknowledged by Ice Camp B",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := &ackFaces{repudiations: c.repudiations}
			if got := f.repudiationFace(); got != c.want {
				t.Fatalf("repudiationFace() = %q, want %q\n"+
					"F6 §5 R2: one dispute is one clause. Repeating an identical clause "+
					"reads as two posts disputing the same trip when one did.", got, c.want)
			}
		})
	}
}
