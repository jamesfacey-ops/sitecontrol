// w3_web_seams_test.go — THE ONE place W3's web tests bind every
// HTTP/rendering spelling the LOCKED criteria leave undetermined
// (design/WINDOW-BUILD-PLAN-v1.md §3 W3, LOCKED 2026-08-27; plan §5
// step 2), plus the shared fixture worlds. No assertions of its own.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD (domain.AmendPerson, PersonAmendment,
// LoadPersonMergeMap, Person.SameAs) — that IS the web suite's clean-tree
// red, captured via go vet.
//
// SHAPE PRESUMPTIONS (SEAMS), each bound once; disputes go to the gate:
//
//   - SEAM W3-W-ROUTE: the officer-facing amendment surface (D-46: an
//     OFFICER action, out from behind dev_routes — the ruling gates the
//     surface, its design stays the builder's) is POST /person/amend,
//     behind login like every authoring route; its PRG confirmation is
//     303 → the authoring surface with ?recorded=<site_id>.<epoch>.<seq>
//     (VF §5 — the parameter name and right-anchored parse are DOC-fixed,
//     the route path is the seam). The page the 303 lands on IS the
//     amendment screen; tests assert against it rather than binding a GET
//     path of their own.
//   - SEAM W3-W-FORM: form fields are named after the payload keys:
//     person_id, name, phone, same_as; the un-merge posts clear=same_as
//     (repeatable `clear` values for the Clear list).
//   - SEAM W3-W-RENDER: rendered-outcome phrases are asserted by VF §5's
//     own load-bearing words, case-insensitively, over markup with
//     style/script stripped (the repo's twice-burned lesson):
//     adverse register  = "superseded" / "not in effect" /
//     "remain merged" / "will not display"
//     losing un-merge   = contains "remain merged" AND the winning
//     post's display name (derived from config)
//     rendering 4       = contains "could not be determined"; the terse
//     success register ("Recorded." / "Effective:")
//     is ABSENT
//     standing line     = the sentence carrying "takes precedence"
//     names the precedence-taker immediately before
//     "'s correction takes precedence" (VF §5's own
//     worked wording at both posts)
//   - SEAM W3-W-CFG: fixture configs mirror the deployed TOMLs' naming
//     (site-a "Ice Road" / site-b "Pad Site Control" [MEASURED — VF §5]);
//     every EXPECTED winner is DERIVED in-test from the config the server
//     actually loads (greater site_id), never hardcoded to a display
//     name — plus a swapped-ids config under which any hardcoded template
//     goes red (T10 / m13).
package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

const (
	w3AmendPath     = "/person/amend" // SEAM W3-W-ROUTE
	w3FieldPersonID = "person_id"     // SEAM W3-W-FORM
	w3FieldName     = "name"
	w3FieldPhone    = "phone"
	w3FieldSameAs   = "same_as"
	w3FieldClear    = "clear"

	w3RemainMergedPhrase = "remain merged"           // SEAM W3-W-RENDER
	w3Rendering4Phrase   = "could not be determined" // VF §5 rendering 4
	w3PrecedencePhrase   = "takes precedence"        // the standing-cost line
)

// w3AdverseTokens is the supersession register (SEAM W3-W-RENDER). The
// standing-cost line's "takes precedence" is static screen text and
// deliberately NOT in this list (VF §5: static text, not a per-POST
// warning — T6b must not be tripped by it).
var w3AdverseTokens = []string{"superseded", "not in effect", "remain merged", "will not display"}

// w3TerseSuccessTokens is the terse success register whose ABSENCE
// rendering 4 demands.
var w3TerseSuccessTokens = []string{"Recorded.", "Effective:"}

func w3PageText(page string) string {
	return strings.ToLower(w2StripNonMarkup(page))
}

func w3HasAdverse(page string) (string, bool) {
	text := w3PageText(page)
	for _, tok := range w3AdverseTokens {
		if strings.Contains(text, tok) {
			return tok, true
		}
	}
	return "", false
}

// --- configs (SEAM W3-W-CFG) --------------------------------------------

func w3ConfigSiteA(dbPath string) *config.Config {
	return &config.Config{
		SiteID: "site-a", DisplayName: "Ice Road", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: dbPath, SyncIntervalSeconds: 3600,
		LoginPassword:  testLoginPassword,
		EntranceSiteID: "site-a",
		Peers:          []config.Peer{{SiteID: "site-b", BaseURL: "http://example.invalid"}},
		Destinations: []config.Destination{
			{ID: "site-b", DisplayName: "Pad Site Control", Controlled: true},
			{ID: "conneco", DisplayName: "Conneco Pad", Controlled: false},
		},
	}
}

func w3ConfigSiteB(dbPath string) *config.Config {
	return &config.Config{
		SiteID: "site-b", DisplayName: "Pad Site Control", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: dbPath, SyncIntervalSeconds: 3600,
		LoginPassword:  testLoginPassword,
		EntranceSiteID: "site-a",
		Peers:          []config.Peer{{SiteID: "site-a", BaseURL: "http://example.invalid"}},
		Destinations: []config.Destination{
			{ID: "site-a", DisplayName: "Ice Road", Controlled: true},
			{ID: "conneco", DisplayName: "Conneco Pad", Controlled: false},
		},
	}
}

// w3ConfigSwap: this post's site_id is GREATER than its peer's — under it
// any hardcoded "Pad Site Control wins" template is red (T10 / m13).
func w3ConfigSwap(dbPath string) *config.Config {
	return &config.Config{
		SiteID: "site-z", DisplayName: "Zulu Post", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: dbPath, SyncIntervalSeconds: 3600,
		LoginPassword:  testLoginPassword,
		EntranceSiteID: "site-z",
		Peers:          []config.Peer{{SiteID: "site-b", BaseURL: "http://example.invalid"}},
		Destinations: []config.Destination{
			{ID: "site-b", DisplayName: "Pad Site Control", Controlled: true},
		},
	}
}

// w3PrecedenceTaker derives the expected precedence-taker rendering from
// the config the server loads: the GREATER site_id's display name — "this
// post" when that is the server itself (VF §5's derivation rule; never a
// hardcoded name).
func w3PrecedenceTaker(cfg *config.Config) string {
	winner := cfg.SiteID
	winnerName := "this post"
	for _, p := range cfg.Peers {
		if p.SiteID > winner {
			winner = p.SiteID
			winnerName = ""
			for _, d := range cfg.Destinations {
				if d.ID == p.SiteID {
					winnerName = d.DisplayName
				}
			}
		}
	}
	return winnerName
}

// --- fixture worlds -----------------------------------------------------

func w3OpenRegistered(t *testing.T, name string) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	st.Clock = w2Reading(w2Base, store.ClockSynced)
	return st
}

// w3SiteAServer: the site-a post with two roster persons authored locally.
func w3SiteAServer(t *testing.T) (*Server, *store.Store, domain.Person, domain.Person) {
	t.Helper()
	st := w3OpenRegistered(t, "w3-site-a.db")
	x, err := domain.CreatePerson(st, "site-a", 1, "Amend Target", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	y, err := domain.CreatePerson(st, "site-a", 1, "Merge Peer", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	return New(w3ConfigSiteA(st.Path), st), st, x, y
}

// w3SiteBServer: the site-b post holding the SAME roster, replicated in.
func w3SiteBServer(t *testing.T, roster []store.Envelope) (*Server, *store.Store) {
	t.Helper()
	st := w3OpenRegistered(t, "w3-site-b.db")
	w3ApplyWire(t, st, roster)
	return New(w3ConfigSiteB(st.Path), st), st
}

// w3Stream reads the (site, epoch) stream from seq 1.
func w3Stream(t *testing.T, st *store.Store, siteID string, epoch int64) []store.Envelope {
	t.Helper()
	evs, err := st.Events(siteID, epoch, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

func w3ApplyWire(t *testing.T, st *store.Store, envs []store.Envelope) {
	t.Helper()
	for _, e := range envs {
		applied, err := st.Apply(e)
		if err != nil {
			t.Fatalf("apply %v: %v (folds never refuse — W3-2)", e.Key(), err)
		}
		if !applied {
			t.Fatalf("apply %v: deduped unexpectedly", e.Key())
		}
	}
}

// w3RemoteAmend authors ONE amendment as siteID on a scratch post primed
// with the given history, at the given clock, and returns its envelope —
// wire-shaped output of a real authoring edge at the other post.
func w3RemoteAmend(t *testing.T, primer []store.Envelope, siteID string, clockMS int64, in domain.PersonAmendment) store.Envelope {
	t.Helper()
	scratch := w3OpenRegistered(t, "w3-scratch-"+siteID+".db")
	w3ApplyWire(t, scratch, primer)
	scratch.Clock = w2Reading(clockMS, store.ClockSynced)
	env, err := domain.AmendPerson(scratch, siteID, 1, in, 0, "default")
	if err != nil {
		t.Fatalf("remote fixture amendment refused: %v", err)
	}
	return env
}

// w3PostAmend posts to the amendment surface with a kept session.
func w3PostAmend(t *testing.T, srv *Server, form url.Values) (*httptest.ResponseRecorder, *http.Cookie) {
	t.Helper()
	return w2PostForm(t, srv, w3AmendPath, form)
}

// w3FollowRecorded asserts the PRG shape (303 + ?recorded=key) and returns
// the confirmation page the officer reads, plus the redirect Location.
func w3FollowRecorded(t *testing.T, srv *Server, rec *httptest.ResponseRecorder, cookie *http.Cookie) (page, location string) {
	t.Helper()
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("amendment POST = %d, want 303 See Other (the PRG confirmation, VF §5): %s", rec.Code, rec.Body.String())
	}
	location = rec.Result().Header.Get("Location")
	if !strings.Contains(location, "recorded=") {
		t.Fatalf("amendment 303 Location %q carries no ?recorded=<site_id>.<epoch>.<seq> — the confirmation GET cannot compute an outcome without it (VF §5)", location)
	}
	next := w2Get(t, srv, location, cookie)
	if next.Code != http.StatusOK {
		t.Fatalf("GET %s after the amendment = %d: %s", location, next.Code, next.Body.String())
	}
	return next.Body.String(), location
}

// w3StandingSentence extracts the sentence carrying the standing-cost
// line's load-bearing phrase.
func w3StandingSentence(t *testing.T, page string) string {
	t.Helper()
	text := w3PageText(page)
	i := strings.Index(text, w3PrecedencePhrase)
	if i < 0 {
		t.Fatalf("W3-6/T10: no standing-cost line (%q) anywhere on the amendment surface — D-28: \"write it where officers will meet it, not just here\". Page:\n%s", w3PrecedencePhrase, page)
	}
	start := strings.LastIndexAny(text[:i], ".<>") + 1
	end := i + len(w3PrecedencePhrase)
	if j := strings.IndexAny(text[end:], ".<"); j >= 0 {
		end += j + 1
	}
	return text[start:end]
}

var w3RecordedParamRe = regexp.MustCompile(`recorded=[^&]*`)

// w3RecordedKey parses the confirmation key out of the 303 Location,
// RIGHT-ANCHORED exactly as VF §5 fixes it: seq and epoch are the last
// two `.`-separated fields; everything left of them is the site_id.
func w3RecordedKey(t *testing.T, location string) store.Key {
	t.Helper()
	u, err := url.Parse(location)
	if err != nil {
		t.Fatal(err)
	}
	v := u.Query().Get("recorded")
	parts := strings.Split(v, ".")
	if len(parts) < 3 {
		t.Fatalf("?recorded=%q does not parse as <site_id>.<epoch>.<seq>", v)
	}
	var epoch, seq int64
	if _, err := fmtSscan(parts[len(parts)-2], &epoch); err != nil {
		t.Fatalf("?recorded=%q: epoch field: %v", v, err)
	}
	if _, err := fmtSscan(parts[len(parts)-1], &seq); err != nil {
		t.Fatalf("?recorded=%q: seq field: %v", v, err)
	}
	return store.Key{SiteID: strings.Join(parts[:len(parts)-2], "."), NodeEpoch: epoch, Seq: seq}
}

func fmtSscan(s string, dst *int64) (int, error) { return fmt.Sscan(s, dst) }
