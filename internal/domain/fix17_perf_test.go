package domain

// fix17_perf_test.go — FIX17 (perf, 2026-10-06): the two read-path changes
// that must give exactly what the old path gave.
//
//   - dump_direct.go decodes projection rows without the JSON round trip.
//     It must equal json.Unmarshal of the canonical dump on every row type
//     the domain reads, and must decline (→ the JSON path) wherever it
//     cannot prove that.
//   - log_follow.go resumes the visit/analytics replay instead of restarting
//     it. A resumed replay must equal a from-zero one after any growth,
//     including growth the follower must refuse to resume over (an earlier
//     epoch growing beneath a later one).

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"sitecontrol/internal/store"
)

// jsonDecodeRows is the path decodeRowsDirect replaces, verbatim in effect:
// the dump's encoding of the rows, then json.Unmarshal into []T.
func jsonDecodeRows[T any](cols []string, rows [][]any) ([]T, error) {
	maps := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		m := map[string]any{}
		for i, c := range cols {
			m[c] = row[i]
		}
		maps = append(maps, m)
	}
	b, err := json.Marshal(maps)
	if err != nil {
		return nil, err
	}
	var out []T
	err = json.Unmarshal(b, &out)
	return out, err
}

func fix17SameDecode[T any](t *testing.T, what string, cols []string, rows [][]any, wantDirect bool) {
	t.Helper()
	direct, ok := decodeRowsDirect[T](cols, rows)
	viaJSON, err := jsonDecodeRows[T](cols, rows)
	if ok != wantDirect {
		t.Errorf("FIX17 direct decode, %s: decoded directly=%v, want %v", what, ok, wantDirect)
	}
	if !ok {
		return
	}
	if err != nil {
		t.Errorf("FIX17 direct decode, %s: decoded directly where json.Unmarshal fails (%v)", what, err)
		return
	}
	if !reflect.DeepEqual(direct, viaJSON) {
		t.Errorf("FIX17 direct decode, %s:\n direct %#v\n json   %#v", what, direct, viaJSON)
	}
}

func TestFix17DirectDecodeEqualsTheJSONPathOrDeclines(t *testing.T) {
	type plain struct {
		ID    string  `json:"id"`
		N     int64   `json:"n"`
		I     int     `json:"i"`
		F     float64 `json:"f"`
		P     *string `json:"p"`
		PN    *int64  `json:"pn"`
		Gone  string  `json:"-"`
		Bare  string
		inner string
	}
	cols := []string{"id", "n", "i", "f", "p", "pn", "Bare", "extra"}
	s := func(v string) any { return v }
	fix17SameDecode[plain](t, "ordinary values", cols, [][]any{
		{s("a"), int64(1), int64(-2), 1.5, s("x"), int64(9), s("b"), s("ignored")},
		{s(""), int64(1 << 62), int64(0), float64(3), s(""), int64(0), s(""), nil},
	}, true)
	fix17SameDecode[plain](t, "NULLs", cols, [][]any{{nil, nil, nil, nil, nil, nil, nil, nil}}, true)
	fix17SameDecode[plain](t, "no rows", cols, [][]any{}, true)
	fix17SameDecode[plain](t, "invalid UTF-8, escapes, NUL", cols, [][]any{
		{s("a\xffb\xc3"), int64(1), int64(1), 0.0, s("<&>\u2028\x00\"\\"), nil, s("\xed\xa0\x80"), nil},
	}, true)

	// Pairings JSON rejects or converts through text: declined, never guessed.
	fix17SameDecode[plain](t, "a string into an integer", cols, [][]any{{s("a"), s("1"), int64(1), 0.0, nil, nil, nil, nil}}, false)
	fix17SameDecode[plain](t, "an integer into a string", cols, [][]any{{int64(1), int64(1), int64(1), 0.0, nil, nil, nil, nil}}, false)
	fix17SameDecode[plain](t, "a real into an integer", cols, [][]any{{s("a"), 2.0, int64(1), 0.0, nil, nil, nil, nil}}, false)
	fix17SameDecode[plain](t, "a blob", cols, [][]any{{[]byte("a"), int64(1), int64(1), 0.0, nil, nil, nil, nil}}, false)
	fix17SameDecode[plain](t, "a key matching only case-insensitively", []string{"ID"}, [][]any{{s("a")}}, false)

	type embedded struct{ plain }
	fix17SameDecode[embedded](t, "an embedded struct", []string{"id"}, [][]any{{s("a")}}, false)
	type stringOpt struct {
		N int64 `json:"n,string"`
	}
	fix17SameDecode[stringOpt](t, "a ,string option", []string{"n"}, [][]any{{s("1")}}, false)
	type named string
	type namedField struct {
		V named `json:"v"`
	}
	fix17SameDecode[namedField](t, "a named field type", []string{"v"}, [][]any{{s("a")}}, false)
	type boolField struct {
		B bool `json:"b"`
	}
	fix17SameDecode[boolField](t, "a bool field", []string{"b"}, [][]any{{int64(1)}}, false)
}

// fix17Season seeds a little of everything the officer pages read: people,
// gate check-ins and check-outs at two posts, a controlled run that arrives.
func fix17Season(t *testing.T, st *store.Store, epochA int64, rounds int) {
	t.Helper()
	veh := map[string]string{}
	vehicle := func(site string, epoch int64, no string) string {
		if id, ok := veh[site+no]; ok {
			return id
		}
		v, err := CreateVehicle(st, site, epoch, &no, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		veh[site+no] = v.VehicleID
		return v.VehicleID
	}
	epochOf := map[string]int64{"site-a": epochA, "site-b": 1}
	var people []Person
	for i := 0; i < 12; i++ {
		site := []string{"site-a", "site-b"}[i%2]
		p, err := CreatePerson(st, site, epochOf[site], fmt.Sprintf("Person %02d", i), nil, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		people = append(people, p)
	}
	dest, mins := "site-a", int64(60)
	for r := 0; r < rounds; r++ {
		for i, p := range people {
			site := []string{"site-a", "site-b"}[i%2]
			trip, err := DispatchLocalCheckIn(st, site, epochOf[site], DispatchInput{VehicleID: vehicle(site, epochOf[site], fmt.Sprintf("T%d", i%3)), DriverPersonID: p.PersonID}, 0, "default")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := RecordExpectedArrival(st, site, epochOf[site], trip.TripID, nil, 0, "default"); err != nil {
				t.Fatal(err)
			}
		}
		open, err := OpenVisitsAt(st, "site-b")
		if err != nil {
			t.Fatal(err)
		}
		for i, v := range open {
			if i == 1 {
				// A run sent and called off: a trip_cancelled for the
				// history's and the moment muster's cancel instants.
				trip, err := Dispatch(st, "site-b", 1, DispatchInput{VehicleID: vehicle("site-b", 1, "CXL"), DriverPersonID: v.PersonID,
					DestinationSite: &dest, DeadlineMinutes: &mins}, 0, "default")
				if err != nil {
					t.Fatal(err)
				}
				if err := CancelTrip(st, "site-b", 1, trip.TripID, nil, 0, "default"); err != nil {
					t.Fatal(err)
				}
				continue
			}
			if i == 0 {
				trip, err := Dispatch(st, "site-b", 1, DispatchInput{VehicleID: vehicle("site-b", 1, "RUN"), DriverPersonID: v.PersonID,
					DestinationSite: &dest, DeadlineMinutes: &mins}, 0, "default")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := RecordExpectedArrival(st, "site-a", epochA, trip.TripID, nil, 0, "default"); err != nil {
					t.Fatal(err)
				}
				continue
			}
			if err := CheckOutVisit(st, "site-b", 1, v.VisitID, nil, 0, "default"); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func fix17SameTable[T any](t *testing.T, st *store.Store, name string) int {
	t.Helper()
	cols, rows, err := st.ProjectionRowsWhere(name)
	if err != nil {
		t.Fatal(err)
	}
	direct, ok := decodeRowsDirect[T](cols, rows)
	if !ok {
		t.Errorf("FIX17 direct decode: %s declined the direct decode — every row type the domain reads must take it", name)
		return 0
	}
	b, err := st.DumpProjectionWhere(name)
	if err != nil {
		t.Fatal(err)
	}
	var viaJSON []T
	if err := json.Unmarshal(b, &viaJSON); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(direct, viaJSON) {
		t.Errorf("FIX17 direct decode: %s decodes differently from its canonical dump", name)
	}
	return len(direct)
}

func TestFix17DirectDecodeEqualsTheDumpOnEveryRowType(t *testing.T) {
	st := newDomainTestStore(t)
	fix17Season(t, st, 1, 3)
	rows := 0
	rows += fix17SameTable[Arrival](t, st, "trip_arrival")
	rows += fix17SameTable[ArrivalNotEntered](t, st, "trip_arrival_not_entered")
	rows += fix17SameTable[ArrivalRefRetractionRow](t, st, "arrival_ref_retraction")
	rows += fix17SameTable[ArrivalResolutionRow](t, st, "arrival_resolution")
	rows += fix17SameTable[ArrivalState](t, st, "arrival_state")
	rows += fix17SameTable[Company](t, st, "company")
	rows += fix17SameTable[OrientationCompletionRow](t, st, "orientation_completion")
	rows += fix17SameTable[OrientationRetractionRow](t, st, "orientation_retraction")
	rows += fix17SameTable[OrientationRow](t, st, "orientation")
	rows += fix17SameTable[Person](t, st, "person")
	rows += fix17SameTable[Trade](t, st, "trade")
	rows += fix17SameTable[Trip](t, st, "trip")
	rows += fix17SameTable[TripCancel](t, st, "trip_cancel")
	rows += fix17SameTable[TripOccupant](t, st, "trip_occupant")
	rows += fix17SameTable[TripRoadWork](t, st, "trip_road_work")
	rows += fix17SameTable[TripState](t, st, "trip_state")
	rows += fix17SameTable[Vehicle](t, st, "vehicle")
	rows += fix17SameTable[Visit](t, st, "visit")
	rows += fix17SameTable[VisitCheckout](t, st, "visit_checkout")
	if rows < 100 {
		t.Fatalf("PRECONDITION: the fixture holds %d rows across the tables — too few to compare anything", rows)
	}
}

// fix17Fresh is the from-zero replay: inside a Transact view the follower is
// a fresh one and nothing is memoised (store.Retained / store.Memo).
// fix17Reads is everything the follower feeds, as the readers return it.
type fix17Reads struct {
	recs     []VisitRecord
	sum      VisitReportSummary
	fig      AnalyticsFigures
	instants TripInstants
	cancels  map[string]RecordedInstant
	moment   *MomentLog
	runs     []DeadlineRun
}

func fix17Read(t *testing.T, st *store.Store) fix17Reads {
	t.Helper()
	var r fix17Reads
	var err error
	if r.recs, r.sum, err = VisitReport(st, SiteTypes{"site-a": SiteTypeTextMessage}); err != nil {
		t.Fatal(err)
	}
	if r.fig, err = BuildAnalytics(st, time.Now(), time.UTC); err != nil {
		t.Fatal(err)
	}
	if r.instants, err = LoadTripInstants(st); err != nil {
		t.Fatal(err)
	}
	if r.cancels, err = cancelInstants(st); err != nil {
		t.Fatal(err)
	}
	if r.moment, err = LoadMomentLog(st, nil); err != nil {
		t.Fatal(err)
	}
	if r.runs, err = DeadlineRuns(st); err != nil {
		t.Fatal(err)
	}
	return r
}

// fix17Fresh reads inside a Transact view: a fresh follower, nothing
// memoised (store.Retained / store.Memo) — the from-zero replay.
func fix17Fresh(t *testing.T, st *store.Store) fix17Reads {
	t.Helper()
	var r fix17Reads
	if err := st.Transact("fix17 fresh replay", func(v *store.Store) error {
		r = fix17Read(t, v)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestFix17AResumedReplayEqualsAFromZeroOne(t *testing.T) {
	st := newDomainTestStore(t)
	compare := func(step string) {
		t.Helper()
		a, b := fix17Read(t, st), fix17Fresh(t, st)
		if len(a.recs) == 0 || len(a.cancels) == 0 || len(a.moment.Runs) == 0 || len(a.runs) == 0 {
			t.Fatalf("PRECONDITION (%s): the fixture must hold visits, runs and a cancel (%d, %d, %d, %d)",
				step, len(a.recs), len(a.moment.Runs), len(a.cancels), len(a.runs))
		}
		for _, c := range []struct {
			what string
			x, y any
		}{
			{"visit report", a.recs, b.recs}, {"visit report summary", a.sum, b.sum},
			{"analytics", a.fig, b.fig}, {"trip instants", a.instants, b.instants},
			{"cancel instants", a.cancels, b.cancels}, {"overdue history runs", a.runs, b.runs},
			{"moment log", *a.moment, *b.moment},
		} {
			if !reflect.DeepEqual(c.x, c.y) {
				t.Fatalf("FIX17 (%s): the resumed %s differs from a from-zero replay:\n resumed %+v\n fresh   %+v", step, c.what, c.x, c.y)
			}
		}
	}
	fix17Season(t, st, 2, 2)
	compare("first replay")
	// Growth at every stream's tail, closing visits the follower already
	// holds (check-outs, a run's dispatch): the resumed path.
	fix17Season(t, st, 2, 2)
	compare("after tail growth")
	// site-a's EARLIER epoch grows beneath the epoch already walked: the
	// follower must not resume over it (a site's epochs replay ascending).
	fix17Season(t, st, 1, 1)
	compare("after an earlier epoch grew")
	fix17Season(t, st, 2, 1)
	compare("after more tail growth")
}
