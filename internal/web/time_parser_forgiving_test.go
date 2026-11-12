// time_parser_forgiving_test.go — table-driven coverage for the forgiving
// clock shapes parseWallTime/parseForgivingClock accept, added as a
// follow-up to the default gate page (reports/witness/DEFAULT-GATE-PAGE.md
// item 8) to bring `occurred_at` to parity with the Access checkin app's
// forgiving HHMM box (design/ACCESS-APP-LAYOUT.md §2).
//
// TWO TABLES ON PURPOSE: a parser tested only on inputs it accepts is half
// tested. The accept table pins each shape to the WALL CLOCK it must
// produce (never just "no error"); the refuse table pins every ambiguous
// or malformed shape named in the brief, so a future edit that makes one
// of them silently guess fails loudly here first.
package web

import (
	"testing"
	"time"
)

// tpcSiteNow is the fixed "today" every forgiving-shape case resolves
// against: an arbitrary afternoon far from any date-rollover edge, so a
// bare time never lands on the wrong calendar day by construction.
var tpcSiteNow = time.Date(2027, time.March, 15, 13, 0, 0, 0, time.Local)

func TestParseWallTimeAcceptsTheStrictWireFormatUnchanged(t *testing.T) {
	got, err := parseWallTime("2026-01-31T14:05", tpcSiteNow)
	if err != nil {
		t.Fatalf("the pre-existing datetime-local wire format must keep working: %v", err)
	}
	want := time.Date(2026, time.January, 31, 14, 5, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseWallTimeAcceptsTheForgivingShapes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		hour int
		min  int
	}{
		{"bare 1-digit hour", "6", 6, 0},
		{"bare 2-digit hour", "18", 18, 0},
		{"3-digit HMM (Access example)", "600", 6, 0},
		{"3-digit HMM, ambiguous-looking but ruled", "130", 1, 30},
		{"4-digit HHMM leading zero (Access example)", "0600", 6, 0},
		{"4-digit HHMM evening", "1800", 18, 0},
		{"H:MM 24h", "6:30", 6, 30},
		{"H:MM 24h evening", "18:05", 18, 5},
		{"Ham lowercase", "6am", 6, 0},
		{"Hpm lowercase", "6pm", 18, 0},
		{"H am with space", "6 am", 6, 0},
		{"H AM uppercase", "6 AM", 6, 0},
		{"H:MMpm (Access example)", "6:30pm", 18, 30},
		{"H:MM pm with space", "6:30 pm", 18, 30},
		{"12am is midnight", "12am", 0, 0},
		{"12pm is noon", "12pm", 12, 0},
		{"noon word (Access example)", "noon", 12, 0},
		{"midnight word", "midnight", 0, 0},
		{"midnight via 0", "0", 0, 0},
		{"midnight via 0000", "0000", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseWallTime(tc.in, tpcSiteNow)
			if err != nil {
				t.Fatalf("parseWallTime(%q) refused: %v — want accepted", tc.in, err)
			}
			y, m, d := tpcSiteNow.Date()
			want := time.Date(y, m, d, tc.hour, tc.min, 0, 0, time.Local)
			if !got.Equal(want) {
				t.Fatalf("parseWallTime(%q) = %v, want %v (today's date, %02d:%02d)", tc.in, got, want, tc.hour, tc.min)
			}
		})
	}
}

// TestParseWallTimeRefusesAmbiguousOrMalformedShapes is the refuse half —
// every case here is either genuinely ambiguous (more than one reading
// fits) or outright malformed, and every one MUST be refused rather than
// guessed: this is an insurance-facing record (brief's own words), and a
// wrong guess here is a wrong time kept in the log forever.
func TestParseWallTimeRefusesAmbiguousOrMalformedShapes(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"13pm: am/pm only names hours 1-12", "13pm"},
		{"2500: hour 25 does not exist", "2500"},
		{"99:99: neither hour nor minute exists", "99:99"},
		{"6x30: not a recognised separator", "6x30"},
		{"whitespace only collapses to blank upstream, not a shape here", "   "},
		{"24:00: hour 24 does not exist in this clock", "24:00"},
		{"6:5: single-digit minute is genuinely ambiguous (6:05 or 6:50)", "6:5"},
		{"0am: am/pm names hours 1-12, not 0", "0am"},
		{"13:00pm: pm on a 24-hour-looking hour", "13:00pm"},
		{"6.30pm: period is not an accepted separator", "6.30pm"},
		{"garbage word", "banana"},
		{"lone colon", ":"},
		{"lone dash", "-"},
		{"5-digit run", "60000"},
		{"a with no hour", "am"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseWallTime(tc.in, tpcSiteNow)
			if err == nil {
				t.Fatalf("parseWallTime(%q) = %v, want a refusal (this shape is ambiguous or malformed, never guessed)", tc.in, got)
			}
		})
	}
}
