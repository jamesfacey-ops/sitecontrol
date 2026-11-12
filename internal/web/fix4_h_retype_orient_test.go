package web

import (
	"strings"
	"testing"
)

// TestGateRetypeKeyByKeyKeepsTheNotOrientedBlock — F-L03-R4-3 typed (fix4,
// 2026-10-05). A picked row's first keystroke goes through showNew, which
// re-syncs the orientation block for the text as it stands (a partial name:
// no block). Every LATER keystroke takes the not-known branch of the row's
// input listener, which only relabelled — so typing on to another unoriented
// person's exact name left the row with no NOT ORIENTED HERE block. Measured
// with Playwright (select-all + type, and backspace + type) at dfd05c1: 0
// blocks; after: 1, naming the person typed. The branch must re-sync, and only
// when the resolved person changes, so a tick already given survives.
func TestGateRetypeKeyByKeyKeepsTheNotOrientedBlock(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	page := orientBlockGet(t, w.srv, "/gate/vehicle")
	branch := sectionBetween(t, page, "else if (!row._known)", "var del = row.querySelector('.p-delete')")
	for _, want := range []string{"exactRosterHit(row)", "syncOrientRow(row, hit)", "orientKey(hit) !== row._orientKey"} {
		if !strings.Contains(branch, want) {
			t.Errorf("the not-known branch of the row's input listener lacks %q — a row retyped key by key to another unoriented person's name loses its NOT ORIENTED HERE block", want)
		}
	}
	if !strings.Contains(page, "row._orientKey = orientKey(it);") {
		t.Error("syncOrientRow does not record which person its block is for — the input listener cannot tell a same-person keystroke from a new person")
	}
}
