// r9_uptime_wiring_test.go — R9 (James, 2026-10-04 — FINDINGS-R4 D5): the
// forward-clock-step check is only alive if the composition root gives the
// deadline scan its second clock. Deadlines.Uptime is nil-safe by design
// (nil = the pre-R9 bridge), so a dropped wiring line would silently put
// back the bug a post restarted at +2 h measured: "OVERDUE 55m16s" at one
// end, "DUE IN 1h4m52s" at the other. A source scan, like
// w7_poke_wiring_test.go beside it.
package main

import (
	"os"
	"strings"
	"testing"

	"sitecontrol/internal/deadline"
)

func TestMainWiresTheR9BootClockIntoTheDeadlineScan(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "alerts.Uptime = deadline.HostUptime") {
		t.Fatal("main.go does not wire alerts.Uptime = deadline.HostUptime — a wall-clock step across a restart moves every in-transit countdown by the step again (D5)")
	}
	// And the clock it wires is real on the deployment platform.
	if _, up, ok := deadline.HostUptime(); !ok || up <= 0 {
		t.Fatalf("deadline.HostUptime() = (%v, %v) on this box — the R9 check would have no second clock", up, ok)
	}
}
