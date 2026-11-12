//go:build linux

// clock_linux.go — the real clock-trust read, on the only platform the
// deployment targets (deploy/build.ps1: CGO_ENABLED=0 GOOS=linux
// GOARCH=amd64).
//
// unix.Adjtimex is a bare Syscall(SYS_ADJTIMEX, ...) in golang.org/x/sys —
// no cgo, so it survives CGO_ENABLED=0 and the one-file deployment story is
// untouched. golang.org/x/sys was already in go.mod (indirect, via modernc's
// sqlite driver); this promotes it to a direct requirement, which is the
// entire dependency cost of the clock-trust read. No new module is
// downloaded and no build tooling changes.
package store

import "golang.org/x/sys/unix"

// readClockTrust asks the kernel, once, what it thinks of this box's clock.
//
// Modes == 0 makes adjtimex a READ-ONLY query: it sets nothing, needs no
// privilege, and cannot disturb a running chrony. Everything below is the
// kernel's own bookkeeping — this code keeps no clock state of its own,
// which is deliberate: "how long has this post been free-running" is a
// question the kernel already answers, and re-deriving it here would be a
// second, worse clock monitor.
func readClockTrust() (int64, ClockState) {
	var tx unix.Timex // Modes == 0: query only, adjust nothing.
	state, err := unix.Adjtimex(&tx)
	if err != nil {
		// The syscall itself failed (seccomp, an exotic kernel). We did not
		// get to ask, so we must not claim either verdict.
		return MaxErrorUnknown, ClockUnverified
	}
	// Two independent ways the kernel says "do not trust this": the
	// STA_UNSYNC status bit, and the TIME_ERROR return value. They normally
	// agree; both are checked because either one alone being set is still
	// the kernel declining to vouch for the clock, and this must fail toward
	// stated uncertainty.
	if tx.Status&unix.STA_UNSYNC != 0 || state == unix.TIME_ERROR {
		// Maxerror is still reported and still meaningful here — it is how
		// far the kernel has already let the bound grow — so it is kept
		// rather than replaced with the unknown sentinel. The state is what
		// says not to trust it.
		return tx.Maxerror, ClockUnsynced
	}
	return tx.Maxerror, ClockSynced
}
