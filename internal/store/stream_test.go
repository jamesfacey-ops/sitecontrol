package store

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

// streamFixture authors three events on (site-a, 1), two on (site-a, 2) and
// one on (site-b, 1) into a bare store — nothing registered, so G13's funnel
// is inert and every payload passes.
func streamFixture(t *testing.T) *Store {
	t.Helper()
	st := newTestStore(t)
	for _, k := range []struct {
		site  string
		epoch int64
		n     int
	}{{"site-a", 1, 3}, {"site-a", 2, 2}, {"site-b", 1, 1}} {
		for i := 0; i < k.n; i++ {
			p, _ := json.Marshal(map[string]any{"type": "probe", "site": k.site, "epoch": k.epoch, "i": i})
			if _, err := st.Author(k.site, k.epoch, p, 0, "default"); err != nil {
				t.Fatal(err)
			}
		}
	}
	return st
}

func streamSeqs(t *testing.T, st *Store, site string, epoch, from, to int64) []int64 {
	t.Helper()
	var out []int64
	if err := st.EachEvent(site, epoch, from, to, func(e Envelope) error {
		if e.SiteID != site || e.NodeEpoch != epoch {
			t.Fatalf("EachEvent(%s,%d) handed an envelope from %v", site, epoch, e.Key())
		}
		out = append(out, e.Seq)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestEachEventStreamsOneStreamInKeyOrderAndHonoursTheBounds pins the
// settled contract: one stream, ascending seq, fromSeq inclusive, toSeq
// inclusive with <= 0 meaning to the end, and an unknown stream yielding
// nothing without error.
func TestEachEventStreamsOneStreamInKeyOrderAndHonoursTheBounds(t *testing.T) {
	st := streamFixture(t)
	if got := streamSeqs(t, st, "site-a", 1, 1, 0); !reflect.DeepEqual(got, []int64{1, 2, 3}) {
		t.Fatalf("(site-a,1) to the end: %v", got)
	}
	if got := streamSeqs(t, st, "site-a", 1, 2, 2); !reflect.DeepEqual(got, []int64{2}) {
		t.Fatalf("(site-a,1) 2..2: %v", got)
	}
	if got := streamSeqs(t, st, "site-a", 1, 2, 0); !reflect.DeepEqual(got, []int64{2, 3}) {
		t.Fatalf("(site-a,1) from 2: %v", got)
	}
	if got := streamSeqs(t, st, "site-a", 2, 1, 0); !reflect.DeepEqual(got, []int64{1, 2}) {
		t.Fatalf("(site-a,2): %v — the second epoch is its own stream", got)
	}
	if got := streamSeqs(t, st, "site-b", 1, 1, 0); !reflect.DeepEqual(got, []int64{1}) {
		t.Fatalf("(site-b,1): %v", got)
	}
	if got := streamSeqs(t, st, "site-c", 1, 1, 0); len(got) != 0 {
		t.Fatalf("unknown stream must yield nothing, got %v", got)
	}
	// The envelope arrives whole: payload bytes and time basis as stored.
	var seen Envelope
	if err := st.EachEvent("site-b", 1, 1, 0, func(e Envelope) error { seen = e; return nil }); err != nil {
		t.Fatal(err)
	}
	want, _, err := st.EventTimeAt(Key{SiteID: "site-b", NodeEpoch: 1, Seq: 1})
	if err != nil {
		t.Fatal(err)
	}
	if seen.AuthoredAt.WallMS != want.AuthoredAtMS || seen.AuthoredAt.State != want.State || !json.Valid(seen.Payload) || seen.Hash() == "" {
		t.Fatalf("envelope not handed over intact: %+v vs %+v", seen, want)
	}
}

// TestEachEventStopsOnTheCallbacksErrorAndReturnsIt — the settled contract's
// third clause: a non-nil error from fn ends the walk there and is returned
// unwrapped, so a consumer's sentinel survives errors.Is.
func TestEachEventStopsOnTheCallbacksErrorAndReturnsIt(t *testing.T) {
	st := streamFixture(t)
	stop := errors.New("stop here")
	var calls int
	err := st.EachEvent("site-a", 1, 1, 0, func(e Envelope) error {
		calls++
		if e.Seq == 2 {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) {
		t.Fatalf("want the callback's error back, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("walk must stop at the erroring event: %d calls", calls)
	}
}

// TestWatermarksListsEveryStreamInSiteEpochOrder — the census a whole-log
// walk iterates; empty store → empty list, no error.
func TestWatermarksListsEveryStreamInSiteEpochOrder(t *testing.T) {
	empty := newTestStore(t)
	if got, err := empty.Watermarks(); err != nil || len(got) != 0 {
		t.Fatalf("empty store: %v, %v", got, err)
	}
	st := streamFixture(t)
	got, err := st.Watermarks()
	if err != nil {
		t.Fatal(err)
	}
	want := []Stream{{"site-a", 1, 3}, {"site-a", 2, 2}, {"site-b", 1, 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("watermarks:\n got %+v\nwant %+v", got, want)
	}
	// Walking every listed stream to the end visits the whole log.
	var total int
	for _, m := range got {
		total += len(streamSeqs(t, st, m.SiteID, m.NodeEpoch, 1, 0))
	}
	if total != 6 {
		t.Fatalf("walking the census visited %d events, want 6", total)
	}
}
