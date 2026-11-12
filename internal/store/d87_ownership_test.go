// d87_ownership_test.go — the D-87 choke point's mechanism, tested in the
// package that owns it and with NO domain knowledge: a probe projection, a
// probe resolver, and the four properties that make the funnel a funnel.
//
// The one that matters most is the LAST test: Apply never consults the
// registry. That is W3-2 stated as a property — a check that fired at fold
// time would refuse a peer's event and this post's own replayed history,
// forking the logs. The mutant it kills is "move refuseForeignRow into
// insertAndFold", which is exactly the shape a well-meaning refactor takes.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
)

// d87Probe opens a store with one probe projection — table probe_row(id,
// site_id), folded from `probe_create` — and one resolver for `probe_modify`
// that reads probe_row's tag. `probe_create` and `probe_free` are
// ACKNOWLEDGED (G13): classified here as not needing a check — probe_create
// mints its own row, probe_free is free reference data — the probe world's
// own miniature of domain's sharedReference/ownMinted/unruledOpen classes.
// Nothing else is registered, so anything outside this three-type closed
// set (e.g. an entirely unclassified probe type) is refused by G13's
// default, exactly as a real unclassified type would be.
func d87Probe(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.RegisterProjection(Projection{
		Name: "probe_row", Table: "probe_row", OrderBy: "id",
		Schema: `CREATE TABLE IF NOT EXISTS probe_row (id TEXT PRIMARY KEY, site_id TEXT NOT NULL) STRICT;`,
		Apply: func(tx *sql.Tx, env Envelope) error {
			var p struct {
				Type string `json:"type"`
				ID   string `json:"id"`
			}
			if err := json.Unmarshal(env.Payload, &p); err != nil || p.Type != "probe_create" {
				return nil
			}
			_, err := tx.Exec(`INSERT OR IGNORE INTO probe_row (id, site_id) VALUES (?, ?)`, p.ID, env.SiteID)
			return err
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.RegisterOwnership("probe_modify", func(tx *sql.Tx, payload json.RawMessage) (string, string, error) {
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return "", "", err
		}
		var owner string
		err := tx.QueryRow(`SELECT site_id FROM probe_row WHERE id = ?`, p.ID).Scan(&owner)
		if errors.Is(err, sql.ErrNoRows) {
			return p.ID, "", nil
		}
		return p.ID, owner, err
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.RegisterAcknowledged("probe_create"); err != nil {
		t.Fatal(err)
	}
	if err := st.RegisterAcknowledged("probe_free"); err != nil {
		t.Fatal(err)
	}
	return st
}

func d87Payload(typ, id string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"type":%q,"id":%q}`, typ, id))
}

func d87MustAuthor(t *testing.T, st *Store, site string, payload json.RawMessage) Envelope {
	t.Helper()
	env, err := st.Author(site, 1, payload, 0, "default")
	if err != nil {
		t.Fatalf("author %s at %s: %v", payload, site, err)
	}
	return env
}

func d87WM(t *testing.T, st *Store, site string) int64 {
	t.Helper()
	wm, err := st.Watermark(site, 1)
	if err != nil {
		t.Fatal(err)
	}
	return wm
}

// TestD87AuthorRefusesAGatedTypeWhoseRowCarriesAnotherSitesTag: the funnel
// refuses with the typed error, names everything, and writes nothing — not
// the log row, not the watermark.
func TestD87AuthorRefusesAGatedTypeWhoseRowCarriesAnotherSitesTag(t *testing.T) {
	st := d87Probe(t)
	d87MustAuthor(t, st, "camp-9", d87Payload("probe_create", "row-1")) // camp-9's row

	_, err := st.Author("camp-7", 1, d87Payload("probe_modify", "row-1"), 0, "default")
	var own *OwnershipError
	if !errors.As(err, &own) {
		t.Fatalf("want *OwnershipError, got %v (%T)", err, err)
	}
	if want := (OwnershipError{EventType: "probe_modify", Target: "row-1", TargetSiteID: "camp-9", SiteID: "camp-7"}); *own != want {
		t.Errorf("refusal = %+v, want %+v", *own, want)
	}
	if wm := d87WM(t, st, "camp-7"); wm != 0 {
		t.Errorf("refused, yet camp-7's watermark is %d", wm)
	}
	if events, _ := st.Events("camp-7", 1, 1, 0); len(events) != 0 {
		t.Errorf("refused, yet camp-7's stream holds %d event(s)", len(events))
	}

	// POSITIVE CONTROLS: the owner may; an ungated type may from anywhere;
	// the resolver's own read errors surface as errors, never as refusals.
	if _, err := st.Author("camp-9", 1, d87Payload("probe_modify", "row-1"), 0, "default"); err != nil {
		t.Errorf("CONTROL: the owning site was refused its own row: %v", err)
	}
	if _, err := st.Author("camp-7", 1, d87Payload("probe_free", "row-1"), 0, "default"); err != nil {
		t.Errorf("CONTROL: an ungated type was refused: %v", err)
	}
}

// TestD87AuthorRefusesAGatedTypeNamingARowUnknownHere: "" from the resolver
// means "no such row here", and that is refused too, with an empty owner.
func TestD87AuthorRefusesAGatedTypeNamingARowUnknownHere(t *testing.T) {
	st := d87Probe(t)
	_, err := st.Author("camp-7", 1, d87Payload("probe_modify", "never-created"), 0, "default")
	var own *OwnershipError
	if !errors.As(err, &own) {
		t.Fatalf("want *OwnershipError, got %v (%T)", err, err)
	}
	if own.TargetSiteID != "" || own.Target != "never-created" {
		t.Errorf("refusal = %+v; want an empty owner and the named id", *own)
	}
	if wm := d87WM(t, st, "camp-7"); wm != 0 {
		t.Errorf("refused, yet camp-7's watermark is %d", wm)
	}
}

// TestD87AResolverErrorIsAnErrorNotARefusal: a read failure inside the
// resolver is surfaced (wrapped, errors.Is-reachable) and is NOT dressed
// as an ownership refusal — the officer must not be told "not yours" when
// the truth is "could not read".
func TestD87AResolverErrorIsAnErrorNotARefusal(t *testing.T) {
	st := d87Probe(t)
	boom := errors.New("probe: disk on fire")
	if err := st.RegisterOwnership("probe_broken", func(*sql.Tx, json.RawMessage) (string, string, error) {
		return "", "", boom
	}); err != nil {
		t.Fatal(err)
	}
	_, err := st.Author("camp-7", 1, d87Payload("probe_broken", "x"), 0, "default")
	if !errors.Is(err, boom) {
		t.Fatalf("want the resolver's error reachable by errors.Is, got %v", err)
	}
	var own *OwnershipError
	if errors.As(err, &own) {
		t.Fatalf("a resolver error was dressed as an ownership refusal: %v", err)
	}
	if wm := d87WM(t, st, "camp-7"); wm != 0 {
		t.Errorf("errored, yet camp-7's watermark is %d", wm)
	}
}

// TestD87RegisterOwnershipRefusesADuplicateAndListsSorted: two resolvers for
// one type would be two opinions about which row it modifies.
func TestD87RegisterOwnershipRefusesADuplicateAndListsSorted(t *testing.T) {
	st := d87Probe(t)
	noop := func(*sql.Tx, json.RawMessage) (string, string, error) { return "", "", nil }
	if err := st.RegisterOwnership("probe_modify", noop); err == nil {
		t.Fatal("a second resolver for probe_modify was accepted")
	}
	if err := st.RegisterOwnership("", noop); err == nil {
		t.Fatal("an empty event type was accepted")
	}
	if err := st.RegisterOwnership("probe_nil", nil); err == nil {
		t.Fatal("a nil resolver was accepted")
	}
	if err := st.RegisterOwnership("a_probe", noop); err != nil {
		t.Fatal(err)
	}
	if got := st.OwnershipGatedTypes(); len(got) != 2 || got[0] != "a_probe" || got[1] != "probe_modify" {
		t.Errorf("OwnershipGatedTypes = %v, want [a_probe probe_modify] (sorted)", got)
	}
}

// TestD87ApplyNeverConsultsTheOwnershipRegistry is W3-2 as a property. A
// delivered envelope from camp-7 that modifies camp-9's row — the exact
// bytes Author refuses — is APPLIED by Apply, folded, and the watermark
// advances: the fold never refuses, a peer's log is stored as received, and
// this post's own replayed history (the self-history rejoin) replays. The
// killer mutant is moving refuseForeignRow into insertAndFold.
func TestD87ApplyNeverConsultsTheOwnershipRegistry(t *testing.T) {
	st := d87Probe(t)
	d87MustAuthor(t, st, "camp-9", d87Payload("probe_create", "row-1"))

	// CONTROL: the same bytes ARE refused at Author, so the pass below is
	// not a registry that never fires.
	if _, err := st.Author("camp-7", 1, d87Payload("probe_modify", "row-1"), 0, "default"); err == nil {
		t.Fatal("CONTROL FAILED: Author accepted the foreign-row payload; the Apply pass below would prove nothing")
	}

	applied, err := st.Apply(Envelope{
		SiteID: "camp-7", NodeEpoch: 1, Seq: 1, SchemaVersion: SchemaVersion, OriginAuth: "camp-7",
		AuthoredAt: AuthoredAt{WallMS: 1, State: ClockUnverified}, OccurredAt: 1, TimeSource: "default",
		Payload: d87Payload("probe_modify", "row-1"),
	})
	if err != nil {
		t.Fatalf("W3-2 VIOLATED: Apply refused a delivered envelope on ownership grounds: %v — the check has leaked into the apply path", err)
	}
	if !applied {
		t.Fatal("the delivered envelope was not applied (watermark issue, not the property under test)")
	}
	if wm := d87WM(t, st, "camp-7"); wm != 1 {
		t.Errorf("applied, yet camp-7's watermark is %d, want 1", wm)
	}
	// And the self-history shape: the same post's OWN prior-epoch stream
	// replays through Apply regardless of what its rows say now.
	applied, err = st.Apply(Envelope{
		SiteID: "camp-7", NodeEpoch: 1, Seq: 2, SchemaVersion: SchemaVersion, OriginAuth: "camp-7",
		AuthoredAt: AuthoredAt{WallMS: 2, State: ClockUnverified}, OccurredAt: 2, TimeSource: "default",
		Payload: d87Payload("probe_modify", "never-created"),
	})
	if err != nil || !applied {
		t.Fatalf("W3-2 VIOLATED on the unknown-row arm: applied=%v err=%v", applied, err)
	}
}
