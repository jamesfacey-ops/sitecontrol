//go:build !linux

// clock_notlinux.go — the honest non-answer on platforms with no adjtimex.
//
// `go test ./...` is run on Windows dev boxes; the deployed binary is
// linux/amd64 only. On a platform where the check is not implemented the
// correct report is "we did not check", NOT "the clock is fine" — the whole
// point of recording trust is that an unchecked clock must never render as a
// confident one. A test run on a dev box therefore stamps every event
// ClockUnverified, which is exactly what it should say.
package store

func readClockTrust() (int64, ClockState) {
	return MaxErrorUnknown, ClockUnverified
}
