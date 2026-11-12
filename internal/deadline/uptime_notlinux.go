//go:build !linux

// uptime_notlinux.go — the honest non-answer on platforms the deployment
// does not target (dev boxes run `go test ./...` on Windows). No second
// clock means the R9 check cannot run; the wall bridge keeps its older
// checks (implausible, backwards) exactly as before.
package deadline

import "time"

func hostUptime() (bootID string, up time.Duration, ok bool) {
	return "", 0, false
}
