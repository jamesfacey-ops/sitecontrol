// clock.go — the AUDIT clock: one wall-clock reading together with the
// kernel's own opinion of how far that reading can be trusted.
//
// This is a DIFFERENT JOB from the deadline clock and must not be confused
// with it. §9.3's arithmetic runs on the monotonic clock and rule 4 forbids
// comparing timestamps from two different clocks; nothing here changes that,
// because nothing here is ever compared against another box's clock. The one
// consumer is Author (log.go), which stamps THIS post's own authored events'
// Envelope time basis (AuthoredAt + rule W's OccurredAt copy) with THIS
// post's own clock, read exactly once per event and only against itself.
// Since SchemaVersion 10 that reading replicates inside the envelope, so it
// is guarded by discipline plus §5B.5's guards rather than structurally:
// nothing may ever compare it against another box's clock.
//
// The trust half exists because the dangerous failure is not a slow clock,
// it is a CONFIDENT WRONG one — a dead RTC battery on a box that has never
// reached NTP produces a perfectly well-formed timestamp that is years out.
// A time whose confidence has to be looked up somewhere else gets read
// without it, so the confidence is recorded in the same row as the time and
// travels with it forever.
package store

import "time"

// ClockState is the kernel's verdict on this box's clock at the moment of a
// reading. Three states, and the third is not a variant of the second:
//
//   - ClockSynced: the kernel considers the clock disciplined. MaxErrorUS is
//     the kernel's own bound and is small.
//   - ClockUnsynced: the kernel itself has given up (STA_UNSYNC / TIME_ERROR)
//     — no NTP has been reached recently enough, or ever. The timestamp is
//     still recorded, because "no time" is worse than "a time marked
//     untrustworthy", but anything rendering it must say so on its face.
//   - ClockUnverified: we could not ask. Non-Linux (the dev/test platform;
//     the deployment target is linux/amd64) or the syscall failed. This is
//     deliberately NOT folded into ClockUnsynced: "the kernel says the clock
//     is bad" and "we never got to ask the kernel" are different facts, and
//     collapsing them would let a build with no clock check masquerade as a
//     box with a known-bad clock, or vice versa.
type ClockState string

const (
	ClockSynced     ClockState = "synced"
	ClockUnsynced   ClockState = "unsynced"
	ClockUnverified ClockState = "unverified"
)

// MaxErrorUnknown is the max_error_us value stored when the kernel would not
// give one (ClockUnverified). It is negative so it can never be mistaken for
// a real microsecond bound by arithmetic that forgets to check the state:
// any comparison of the form "error <= tolerance" would WRONGLY pass on a
// zero sentinel, and passing is the dangerous direction.
const MaxErrorUnknown int64 = -1

// ClockReading is one atomic-enough read of the wall clock and its trust.
// The two halves are produced together, on purpose: the point of this type
// is that a timestamp never exists in this system without its confidence
// beside it.
type ClockReading struct {
	// WallMS is Unix milliseconds. Milliseconds, not seconds, only so the
	// value has the same unit as every other persisted wall time in the tree
	// (deadline's receipt_wall_ms, fired_wall_ms). Nothing here needs
	// sub-second resolution; a muster is answered to the minute.
	WallMS int64

	// MaxErrorUS is the kernel's maximum-error estimate in MICROseconds
	// (unix.Timex.Maxerror), or MaxErrorUnknown. This is the single most
	// useful number in the row and the reason the design does not need a
	// clock-monitoring subsystem of its own: the kernel GROWS it
	// automatically during free-run at its assumed drift rate, so it
	// degrades continuously — milliseconds while chrony is disciplining,
	// seconds after a day off the link — rather than as a cliff. When it
	// exceeds the kernel's tolerance the kernel sets STA_UNSYNC itself, and
	// State reports that.
	MaxErrorUS int64

	// State is the verdict. Read this BEFORE MaxErrorUS.
	State ClockState
}

// Clock is the injectable reading function. Production wires SystemClock;
// tests substitute a fixed or deliberately-hostile reading (a dead-RTC box
// with a never-disciplined clock is otherwise untestable without a real
// dead RTC).
type Clock func() ClockReading

// SystemClock reads the wall clock and asks the kernel how far to trust it.
// One time.Now and one adjtimex, in that order; the ordering is immaterial
// (the trust bound moves on a scale of minutes) and neither can block.
func SystemClock() ClockReading {
	now := time.Now().UnixMilli()
	maxErr, state := readClockTrust()
	return ClockReading{WallMS: now, MaxErrorUS: maxErr, State: state}
}
