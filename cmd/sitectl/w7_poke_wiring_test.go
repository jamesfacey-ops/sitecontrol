// w7_poke_wiring_test.go — W7 / Y-17: the §8.9 poke is only alive if the
// composition root wires store.OnAuthored to the engine. main.go claimed for
// a year that "authored events poke peers via engine.Author", which nothing
// in the product called (sc-test L12 Y-17: tick-only replication, 3-17 s).
// A source scan, like the citation tests beside it: the subprocess tests in
// this package cannot observe a poke without a second live post.
package main

import (
	"os"
	"strings"
	"testing"
)

func TestMainWiresTheAuthoringPokeThroughTheStoreHook(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	if !strings.Contains(s, "st.OnAuthored = engine.PokePeers") {
		t.Fatal("main.go does not wire st.OnAuthored = engine.PokePeers — the §8.9 poke fast path is dead again (Y-17)")
	}
	if strings.Contains(s, "poke peers via engine.Author") || strings.Contains(s, "peers via engine.Author (the §8.9 fast path") {
		t.Fatal("main.go still claims the poke runs via engine.Author, which nothing in the product calls")
	}
}
