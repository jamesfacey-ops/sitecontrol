// fix8_row_times_test.go — ⟦FIX8, James, 2026-10-05⟧ "check in and out on
// each entry on differing times". Each ticked roster row carries its own
// optional Time out box; blank means the form-level time (now unless
// changed); the vehicle forms keep one time, labelled for the whole vehicle.
// See gate_rowtime.go.
package web

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

func fix8Wall(ms int64) string { return time.UnixMilli(ms).In(time.Local).Format(w2WallFmt) }

// fix8World checks Dana, Pat and Chris in ONE AT A TIME at three different
// entered times in (3h, 2h30 and 2h before now) and returns their visit ids
// in that order with the time-in instants.
func fix8World(t *testing.T) (*Server, *store.Store, []string, []int64) {
	t.Helper()
	srv, st, persons := newW2Server(t, w2Base-6*60*w2Minute)
	ins := []int64{w2Base - 180*w2Minute, w2Base - 150*w2Minute, w2Base - 120*w2Minute}
	for i, p := range persons {
		rec, _ := w2PostForm(t, srv, "/gate/checkin", url.Values{
			"time_source": {"entered"}, "occurred_at": {fix8Wall(ins[i])},
			"vehicle": {"TRK-F8-" + string(rune('A'+i))}, "driver_kind": {"existing"}, "driver_value": {p.PersonID},
		})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("check-in %d = %d: %s", i, rec.Code, rec.Body.String())
		}
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	byPerson := map[string]string{}
	for _, v := range open {
		byPerson[v.PersonID] = v.VisitID
	}
	var vids []string
	for _, p := range persons {
		if byPerson[p.PersonID] == "" {
			t.Fatalf("%s has no open visit", p.Name)
		}
		vids = append(vids, byPerson[p.PersonID])
	}
	return srv, st, vids, ins
}

// fix8Checkouts maps visit id -> its visit_checked_out envelope.
func fix8Checkouts(t *testing.T, st *store.Store, after int64) map[string]store.Envelope {
	t.Helper()
	out := map[string]store.Envelope{}
	for _, e := range w2EventsOfType(t, w2EventsFrom(t, st, "site-a", 1, after), "visit_checked_out") {
		p := string(e.Payload)
		i := strings.Index(p, `"visit_id":"`)
		if i < 0 {
			t.Fatalf("checkout payload with no visit_id: %s", p)
		}
		rest := p[i+len(`"visit_id":"`):]
		out[rest[:strings.Index(rest, `"`)]] = e
	}
	return out
}

func TestFix8EachTickedRowStoresItsOwnTimeOut(t *testing.T) {
	srv, st, vids, _ := fix8World(t)
	before := w2Watermark(t, st, "site-a", 1)
	outA, outB := w2Base-60*w2Minute, w2Base-30*w2Minute
	rec, _ := w2PostForm(t, srv, "/gate/checkout", url.Values{
		"time_source": {"default"}, "visit_ids": {vids[0], vids[1]},
		"time_out_" + vids[0]: {fix8Wall(outA)}, "time_out_" + vids[1]: {fix8Wall(outB)},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("check-out = %d: %s", rec.Code, rec.Body.String())
	}
	got := fix8Checkouts(t, st, before)
	for vid, want := range map[string]int64{vids[0]: outA, vids[1]: outB} {
		e, ok := got[vid]
		if !ok {
			t.Fatalf("no visit_checked_out for %s", vid)
		}
		if e.TimeSource != "entered" || e.OccurredAt != want {
			t.Fatalf("visit %s: stored (%d, %q), want (%d, \"entered\") — the row's own Time out never reached its event", vid, e.OccurredAt, e.TimeSource, want)
		}
	}
}

func TestFix8BlankRowMeansTheFormLevelTime(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		srv, st, vids, _ := fix8World(t)
		before := w2Watermark(t, st, "site-a", 1)
		rec, _ := w2PostForm(t, srv, "/gate/checkout", url.Values{
			"time_source": {"default"}, "visit_ids": {vids[0], vids[1]},
			"time_out_" + vids[0]: {""}, "time_out_" + vids[1]: {fix8Wall(w2Base - 10*w2Minute)},
		})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("check-out = %d: %s", rec.Code, rec.Body.String())
		}
		got := fix8Checkouts(t, st, before)
		if e := got[vids[0]]; e.TimeSource != "default" {
			t.Fatalf("blank row stored time_source %q, want default (now)", e.TimeSource)
		}
		if e := got[vids[1]]; e.TimeSource != "entered" || e.OccurredAt != w2Base-10*w2Minute {
			t.Fatalf("filled row stored (%d, %q)", e.OccurredAt, e.TimeSource)
		}
	})
	// The scripting-off apply-to-all: a changed form-level time applies to
	// every ticked row whose own box is blank; a filled box still wins.
	t.Run("form-level entered applies to blank rows", func(t *testing.T) {
		srv, st, vids, _ := fix8World(t)
		before := w2Watermark(t, st, "site-a", 1)
		all, own := w2Base-45*w2Minute, w2Base-5*w2Minute
		rec, _ := w2PostForm(t, srv, "/gate/checkout", url.Values{
			"time_source": {"entered"}, "occurred_at": {fix8Wall(all)},
			"visit_ids":           {vids[0], vids[1], vids[2]},
			"time_out_" + vids[2]: {fix8Wall(own)},
		})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("check-out = %d: %s", rec.Code, rec.Body.String())
		}
		got := fix8Checkouts(t, st, before)
		for vid, want := range map[string]int64{vids[0]: all, vids[1]: all, vids[2]: own} {
			if e := got[vid]; e.TimeSource != "entered" || e.OccurredAt != want {
				t.Fatalf("visit %s stored (%d, %q), want (%d, entered)", vid, e.OccurredAt, e.TimeSource, want)
			}
		}
	})
}

func TestFix8RowTimeRefusalsRecordNothing(t *testing.T) {
	cases := []struct {
		name string
		form func(vids []string) url.Values
		want string
	}{
		{"future beyond the allowance", func(v []string) url.Values {
			return url.Values{"time_source": {"default"}, "visit_ids": {v[0], v[1]},
				"time_out_" + v[1]: {fix8Wall(w2Base + 30*w2Minute)}}
		}, "ahead of this box's clock"},
		{"time out before that person's time in", func(v []string) url.Values {
			return url.Values{"time_source": {"default"}, "visit_ids": {v[0], v[2]},
				"time_out_" + v[2]: {fix8Wall(w2Base - 170*w2Minute)}} // Chris came in at -120
		}, "earlier than their time in"},
		{"a time typed on an unticked row", func(v []string) url.Values {
			return url.Values{"time_source": {"default"}, "visit_ids": {v[0]},
				"time_out_" + v[1]: {fix8Wall(w2Base - 20*w2Minute)}}
		}, "not ticked"},
		{"an unreadable row time", func(v []string) url.Values {
			return url.Values{"time_source": {"default"}, "visit_ids": {v[0]},
				"time_out_" + v[0]: {"6x30"}}
		}, "could not be read"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv, st, vids, _ := fix8World(t)
			before := w2Watermark(t, st, "site-a", 1)
			rec, _ := w2PostForm(t, srv, "/gate/checkout", c.form(vids))
			if rec.Code < 400 || rec.Code > 499 {
				t.Fatalf("= %d, want a 4xx refusal: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), c.want) || !strings.Contains(rec.Body.String(), "Nothing has been recorded") {
				t.Fatalf("refusal does not say %q and that nothing was recorded: %s", c.want, rec.Body.String())
			}
			if after := w2Watermark(t, st, "site-a", 1); after != before {
				t.Fatalf("a refused check-out authored %d event(s)", after-before)
			}
		})
	}
}

// The departing-vehicle door keeps ONE time, and it is held to the same
// out-before-in rule against everybody aboard.
func TestFix8VehicleTimeOutBeforeTimeInIsRefused(t *testing.T) {
	srv, st, vids, _ := fix8World(t)
	_, vals := gateDepartSelected(t, srv, []string{vids[2]}) // Chris, in at -120
	vals.Set("vehicle", "TRK-F8-C")
	vals.Set("destination_id", "conneco")
	vals = gateMarkDriver(vals, 0)
	vals.Set("time_source", "entered")
	vals.Set("occurred_at", fix8Wall(w2Base-170*w2Minute))
	before := w2Watermark(t, st, "site-a", 1)
	rec, _ := w2PostForm(t, srv, "/gate/checkout/vehicle", vals)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "earlier than their time in") {
		t.Fatalf("vehicle out before time in = %d, want 409 naming the rule: %s", rec.Code, rec.Body.String())
	}
	if after := w2Watermark(t, st, "site-a", 1); after != before {
		t.Fatalf("refused departure authored %d event(s)", after-before)
	}
	// The same vehicle at a time AFTER the time in is accepted, entered.
	vals.Set("occurred_at", fix8Wall(w2Base-20*w2Minute))
	if rec, _ := w2PostForm(t, srv, "/gate/checkout/vehicle", vals); rec.Code != http.StatusSeeOther {
		t.Fatalf("vehicle out after time in = %d: %s", rec.Code, rec.Body.String())
	}
}

// "Check out selected" stages the ticked people onto the departing vehicle
// and carries each one's own Time out onto their staged row (option C: the
// commit then records one departure per time — fix8_split_test.go).
func TestFix8CheckOutSelectedCarriesRowTimes(t *testing.T) {
	srv, _, vids, _ := fix8World(t)
	a, b := fix8Wall(w2Base-15*w2Minute), fix8Wall(w2Base-25*w2Minute)
	rec, _ := w2PostForm(t, srv, "/gate/checkout/select", url.Values{
		"visit_ids": {vids[0], vids[1]}, "time_out_" + vids[0]: {a}, "time_out_" + vids[1]: {b},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("/gate/checkout/select = %d", rec.Code)
	}
	vals := gateFormValues(t, rec.Body.String(), formIDFor(gateOutbound))
	if got := vals["occupant_time"]; len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("staged rows carry occupant_time %v, want [%s %s]", got, a, b)
	}
}

// Every row's box is labelled per person; the vehicle forms say their one
// time is for everyone aboard; the individual check-in's box says Time in.
func TestFix8TimeBoxesAreLabelled(t *testing.T) {
	srv, _, _, _ := fix8World(t)
	_, veh := getGate(t, srv)
	for _, want := range []string{
		`aria-label="Time out for Chris Crew"`,
		`name="time_out_`,
		"Time in for everyone not given their own",
		"Someone arrived at a different time?",
		"Time out for everyone not given their own",
	} {
		if !strings.Contains(veh, want) {
			t.Fatalf("/gate/vehicle lacks %q", want)
		}
	}
	_, ind := getGateIndividual(t, srv)
	for _, want := range []string{"Time in (if not now)", `<strong class="time-legend">Time in</strong>`, `aria-label="Time out for Dana Driver"`} {
		if !strings.Contains(ind, want) {
			t.Fatalf("/gate lacks %q", want)
		}
	}
}

// Replication: the per-person times are ordinary envelope pairs, so a
// second store that receives site-a's stream holds — and reports — them.
func TestFix8RowTimesReplicateToTheOtherPost(t *testing.T) {
	srv, st, vids, ins := fix8World(t)
	outA, outB := w2Base-70*w2Minute, w2Base-40*w2Minute
	rec, _ := w2PostForm(t, srv, "/gate/checkout", url.Values{
		"time_source": {"default"}, "visit_ids": {vids[0], vids[1]},
		"time_out_" + vids[0]: {fix8Wall(outA)}, "time_out_" + vids[1]: {fix8Wall(outB)},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("check-out = %d: %s", rec.Code, rec.Body.String())
	}
	other, err := store.Open(filepath.Join(t.TempDir(), "post-b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close() })
	if err := domain.Register(other); err != nil {
		t.Fatal(err)
	}
	evs, err := st.Events("site-a", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.ApplyBatch(evs); err != nil {
		t.Fatal(err)
	}
	recs, _, err := domain.VisitReport(other, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][2]int64{vids[0]: {ins[0], outA}, vids[1]: {ins[1], outB}}
	seen := 0
	for _, r := range recs {
		w, ok := want[r.VisitID]
		if !ok {
			continue
		}
		seen++
		if r.ArrivedAtMS != w[0] || r.ClosedAtMS != w[1] {
			t.Fatalf("the other post reports visit %s as in %d / out %d, want %d / %d", r.VisitID, r.ArrivedAtMS, r.ClosedAtMS, w[0], w[1])
		}
		if r.OnSiteMinutes == nil || *r.OnSiteMinutes != (w[1]-w[0])/w2Minute {
			t.Fatalf("the other post's on-site minutes for %s = %v, want %d", r.VisitID, r.OnSiteMinutes, (w[1]-w[0])/w2Minute)
		}
	}
	if seen != 2 {
		t.Fatalf("the other post's visit report holds %d of the 2 checked-out visits", seen)
	}
}
