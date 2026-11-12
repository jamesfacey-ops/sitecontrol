//go:build linux

// uptime_linux.go — the host's boot identity and time-since-boot, the R9
// witness's second clock (see clockstep.go). Same dependency story as
// store/clock_linux.go: golang.org/x/sys/unix is a bare syscall, no cgo.
package deadline

import (
	"os"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// hostUptime reads CLOCK_BOOTTIME (monotonic, counts suspend, never stepped
// by settimeofday or an NTP step — the property the whole check rests on)
// and the kernel's per-boot random id, which tells "the process restarted"
// from "the host rebooted". ok=false when either is unreadable: the caller
// then has no second clock and keeps the wall bridge's older checks only.
func hostUptime() (bootID string, up time.Duration, ok bool) {
	b, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", 0, false
	}
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_BOOTTIME, &ts); err != nil {
		return "", 0, false
	}
	id := strings.TrimSpace(string(b))
	if id == "" {
		return "", 0, false
	}
	return id, time.Duration(ts.Nano()), true
}
