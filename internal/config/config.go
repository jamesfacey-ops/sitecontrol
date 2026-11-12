// Package config loads and validates the per-site TOML configuration.
//
// One binary, config-only differentiation (chunk-plan §6): every instance of
// sitectl is the same executable; site identity, epoch, peers, and all
// site-local knobs come exclusively from this file. Site config is local-only,
// seeded at provisioning, and belongs to no replication class (design §7).
package config

import (
	"fmt"
	"hash/fnv"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"sitecontrol/internal/domain"
)

// Peer is one entry in the pull mesh: who we pull from and the demo key we
// present. Nothing may assume exactly two peers (chunk-plan C4 note).
type Peer struct {
	SiteID  string `toml:"site_id"`
	BaseURL string `toml:"base_url"`
	Key     string `toml:"key"`
	// DisplayName is how THIS post names the peer on screen. Optional.
	// James, 2026-10-04 (FINDINGS-R4 cluster I): a post with no destinations
	// has no Destination row to take a name from, so before this field it
	// printed the peer's raw id ("added at ice-pad"). A peer name is
	// display only: ids on the wire and in URLs never change.
	DisplayName string `toml:"display_name"`
	// LongName is the peer's full name, shown where a long name belongs
	// (its site page heading). Optional. See Config.LongName.
	LongName string `toml:"long_name"`
	// RoadWorkLabel is what the PEER calls its working-on-the-road category
	// (its own [road_work] display_name), for a post that shows the peer's
	// road-work runs but offers no such category itself — the dispatch
	// post's overview tile (FIX15, James 2026-10-06: from NDB it read
	// "Working on the road" while the ice-road post sends "Working on the
	// ice road"). Optional and display only: the label is not replicated
	// (a trip carries road_work: true, nothing more), so the post that
	// shows it is told it here, like every other display name.
	RoadWorkLabel string `toml:"road_work_label"`
	// SiteType is the PEER's own site_type (its TOML's `site_type`), for a
	// post that reports on the peer's sign-outs — the dispatch view's
	// missed sign-outs report (FIX16f, James 2026-10-06: "broken down by
	// site as well as for dispatch as a whole"). Optional: a post knows its
	// OWN type and no peer's (config is local-only, visit_report.go "WHAT A
	// POST KNOWS"), so without this line the report names the peer as
	// untyped and gives its returns no verdict rather than guess. Not
	// replicated, like every other display fact here; checked against
	// domain.ParseSiteType when present.
	SiteType domain.SiteType `toml:"site_type"`
}

// FormerPeer names a post that has been RETIRED from this mesh (FIX16 H,
// `sitectl admin peer remove`; James-approved 2026-10-06). Its records stay
// in every replica's append-only log — history is never deleted — but it is
// no longer pulled from or dispatched to, so it is not a [[peers]] entry any
// more. Without this table its records would fall back to the raw site id
// on every screen (SiteName's last resort). Display only: nothing here is
// replicated, keyed or contacted.
type FormerPeer struct {
	SiteID      string `toml:"site_id"`
	DisplayName string `toml:"display_name"`
	LongName    string `toml:"long_name"`
}

// Destination is one place a truck can be dispatched to from this site —
// pads come and go, so the list is configuration, never code (James,
// 2026-08-20). Two kinds:
//
//   - Controlled: a site control post with its own gate — a configured peer.
//     Dispatching there is an inbound expectation: deadline REQUIRED, timer
//     runs, appears on that site's inbound board, arrival there closes it.
//   - Uncontrolled (e.g. the Conneco pad): no gate, no officer, nobody who
//     can record an arrival. Destination and occupants are RECORDED and the
//     occupants are checked out of the origin, but there is no deadline, no
//     timer and no alert — a promise nobody can close would only ever
//     produce a false alarm (the same reasoning as F4, one notch more
//     precise).
//
// validate() reconciles this list with Peers at startup — see there.
type Destination struct {
	ID          string `toml:"id"`
	DisplayName string `toml:"display_name"` // defaults to ID when empty
	Controlled  bool   `toml:"controlled"`

	// TravelMinutes is the published travel time to this destination, in
	// whole minutes — ONE number per road segment, the same in both
	// directions (James, 2026-10-03: the ice road is 24 miles at 25 mph, so
	// 65 minutes each way). It prefills the dispatch form's deadline; the
	// officer may change it per run. 0 (omitted) means no prefill; negative
	// is refused at startup. The scan never reads it: a trip's allowance is
	// the deadline the officer recorded.
	//
	// ⟦was: `map[string]int64`, a per-WEATHER-PHASE table keyed "0"/"2"/"3"
	// ({ 0 = 65, 2 = 65, 3 = 240 }) that scaled in-flight deadlines when the
	// board changed phase. Weather phases were removed, James 2026-10-04:
	// "the phase conditions can be removed they will not effect site control
	// … phase 1 and 2 are already going to be 25 MPH so that wont change and
	// phase 3 is no movement anyway". A config still writing the table form
	// is REFUSED by Load (D-77), never guessed at — see refuseRemovedWeatherConfig.⟧
	TravelMinutes int64 `toml:"travel_minutes"`
}

// ⟦was: WeatherPhase and Weather — the [[weather.phase]] table (phase,
// display_name, dispatch_allowed, review_in_transit). Removed with weather
// phases, James 2026-10-04; a config still carrying the table is refused by
// Load — see refuseRemovedWeatherConfig.⟧

// RoadWork is the [road_work] table — James, 2026-10-03: "the ice road needs
// another category for equipment working on the ice road so that they are
// not listed as overdue when they will spend up to 12 hours on the road
// working, so once 12 hours hits they will be labelled overdue."
//
// Equipment sent out WORKING ON THE ROAD has no destination post: it leaves
// this gate, is listed in its own category everywhere a trip is listed, and
// is overdue only once it has been out longer than LimitMinutes. The limit is
// CONFIG, not a magic number and not a per-dispatch field — the officer does
// not type it; this post's file decides it, exactly as the travel tables
// decide a trip's prefill. PRESENCE of the table is what offers the category
// at this post at all: a post with no [road_work] block offers no such
// choice (the demo's entry-only posts and the pad have none; the ice road
// has one). Read it through RoadWorkEnabled / RoadWorkLimitMinutes /
// RoadWorkLabel, never the pointer.
type RoadWork struct {
	// LimitMinutes is how long equipment may be out working before it is
	// OVERDUE. 0 (or an omitted key) means the default, 720 — twelve hours,
	// James's number. Negative is refused at startup.
	LimitMinutes int64 `toml:"limit_minutes"`
	// DisplayName is the category's label on every screen — the officer's
	// button, the board section, the muster heading. Defaults to "Working on
	// the road"; the demo spells it "Working on the ice road". A road name
	// is a label for the officer, not a site identity, so it lives in config
	// the way every other display name does (D-77: nothing in source names
	// a site).
	DisplayName string `toml:"display_name"`
}

// DefaultRoadWorkLimitMinutes is the limit [road_work] applies when
// limit_minutes is omitted or 0: twelve hours (James, 2026-10-03).
const DefaultRoadWorkLimitMinutes int64 = 720

// RoadWorkChoice is the destination_id value the dispatch forms submit for
// "working on the road" — one <select>/button set serves every way a vehicle
// can leave, so the choice rides the same field as a real destination and is
// resolved by the same function (web/trips.go applyDispatchDestination). It
// is RESERVED: validate() refuses a [[destinations]] entry with this id, so
// the two can never collide on a form.
const RoadWorkChoice = "road-work"

// defaultRoadWorkLabel is RoadWork.DisplayName's fallback.
const defaultRoadWorkLabel = "Working on the road"

// RoadWorkEnabled reports whether this post offers the working-on-the-road
// category: true exactly when the file carries a [road_work] table.
func (c *Config) RoadWorkEnabled() bool { return c.RoadWork != nil }

// RoadWorkLimitMinutes is the configured working limit, defaulted — the
// deadline every road-work dispatch at this post is stamped with. 0 when the
// category is not offered here (callers check RoadWorkEnabled first; a
// zero deadline is refused by domain.Dispatch anyway, so the unsafe path is
// closed twice).
func (c *Config) RoadWorkLimitMinutes() int64 {
	if c.RoadWork == nil {
		return 0
	}
	if c.RoadWork.LimitMinutes <= 0 {
		return DefaultRoadWorkLimitMinutes
	}
	return c.RoadWork.LimitMinutes
}

// RoadWorkLabel is the category's display name, defaulted. Always non-empty
// (a surface may name the category for a REPLICATED road-work trip even at
// a post that does not offer the choice itself).
func (c *Config) RoadWorkLabel() string {
	if c.RoadWork != nil && strings.TrimSpace(c.RoadWork.DisplayName) != "" {
		return strings.TrimSpace(c.RoadWork.DisplayName)
	}
	return defaultRoadWorkLabel
}

// Config is the whole per-site file. Field notes reference the design doc.
type Config struct {
	SiteID      string `toml:"site_id"`
	DisplayName string `toml:"display_name"`
	// LongName is this post's full name where a long name belongs — the
	// top bar's name slot and the site page's heading ("Salt Water
	// Treatment Plant" for STP). Optional; DisplayName stays the short name
	// used inline. James, 2026-10-04 (FINDINGS-R4 cluster I).
	LongName   string `toml:"long_name"`
	ListenAddr string `toml:"listen_addr"`

	// NodeEpoch is seeded here at provisioning/re-image and NEVER inferred
	// (design §8.2). Part of the ordering/idempotency/watermark key from C2 on.
	NodeEpoch int64 `toml:"node_epoch"`

	// PeerKey is the demo key THIS site expects peers to present (design §8.4;
	// demo keys are sanctioned). Real credential handling is production-doc material.
	PeerKey string `toml:"peer_key"`

	Peers []Peer `toml:"peers"`

	// FormerPeers names retired posts so their records keep their names
	// (see FormerPeer). OPTIONAL; written by `sitectl admin peer remove`.
	FormerPeers []FormerPeer `toml:"former_peers"`

	// Destinations is the dispatch-destination list (one button each in the
	// C6 dispatch form). OPTIONAL only while this post has AT MOST ONE peer:
	// then DestinationList() derives one CONTROLLED destination for it —
	// exactly the pre-destinations behaviour, where the dispatch <select> was
	// built from the peer list, and correct because one peer is one road.
	// With MORE than one peer the block is REQUIRED and validate() refuses
	// its absence at startup (D-77, 2026-09-05): "who I replicate with" and
	// "who I can drive to" are different lists the moment a third site
	// exists, and a per-peer default would arm a deadline toward every peer.
	// Validated against Peers at startup (validate()).
	Destinations []Destination `toml:"destinations"`

	// destinationsNone is set by Load when the file says `destinations = []`
	// — an EXPLICIT "this post has no travel destinations". It is the only
	// way to write "none" at a post with peers: an omitted key derives (one
	// peer) or is refused (two or more, D-77), and an omitted key must never
	// be confused with a declared-empty one. When set, DestinationList()
	// returns nothing and NEVER derives from peers. Unexported: not a TOML key.
	destinationsNone bool

	// LoginHint is an OPTIONAL demo aid. When non-empty the login page shows
	// "Demo login: <LoginHint>". Unset = nothing rendered. Never set it on a
	// real deployment: it prints the site password's whereabouts to anyone
	// who can reach the login page.
	LoginHint string `toml:"login_hint"`

	// ProxyAuthToken is an OPTIONAL demo aid. When non-empty, a request
	// carrying header X-SC-Proxy-Auth equal to it is treated as logged in
	// (web.authedSession mints the normal session). Unset = the header is
	// ignored and behaviour is unchanged. Only trustworthy behind a local
	// proxy that overwrites the header, so validate() refuses it unless
	// listen_addr is a loopback address. Never set on a real deployment.
	ProxyAuthToken string `toml:"proxy_auth_token"`

	// MusterMinGroupSize is numeric on purpose — the JET "13"<"2" text-compare
	// failure is the named regression this guards against (chunk-plan C9).
	MusterMinGroupSize int `toml:"muster_min_group_size"`

	SyncIntervalSeconds int    `toml:"sync_interval_seconds"`
	DBPath              string `toml:"db_path"`

	// DevRoutes gates the C5 dev-only alias-injection route (chunk-plan C5,
	// M1). Default OFF (Go zero value = false); set true only in the demo
	// deploy configs (deploy/site-*.toml). With this false the route is not
	// registered at all, so it answers 404 — not merely "disabled" behind a
	// runtime check. Once C11 (site login) exists, the route additionally
	// sits behind it; C5 does not add per-action auth itself.
	DevRoutes bool `toml:"dev_routes"`

	// ⟦was: Weather Weather `toml:"weather"` — the board's phase table;
	// removed with weather phases, James 2026-10-04⟧

	// RoadWork (James, 2026-10-03) is the OPTIONAL [road_work] table: present
	// at a post whose officer can send equipment out WORKING ON THE ROAD —
	// no destination post, listed in its own category, overdue after the
	// configured limit (default twelve hours). Absent at every other post;
	// see RoadWork's own comment. A pointer so that "offered here" and "not
	// offered here" are distinguishable from a zero-valued struct.
	RoadWork *RoadWork `toml:"road_work"`

	// LoginPassword is the single per-site credential C11 checks a submitted
	// /login form against (design §12, finding F4: "login mechanics are
	// absent... one config credential per site"). There is deliberately no
	// user table and no per-officer password — accountability is carried by
	// the POST identity this box carries (D-5's recorded_by — see
	// PostName), not by this credential, which only gates "is this browser
	// allowed to use this site at all." Plaintext in config is the same posture already taken
	// for PeerKey above; see the C11 build report's security-posture note
	// for what this credential does and does not defend against.
	LoginPassword string `toml:"login_password"`

	// EntranceSiteID names the post that is the SITE'S ONLY ENTRANCE. James:
	// "all intake should theoretically be at ice road entrance as the only
	// way to the pad is through the ice road."
	//
	// It changes NOTHING about what any post may do — every post keeps full
	// intake capability, because a post that physically cannot enrol somebody
	// standing in front of it is a worse failure than one that can and says
	// so ("i wouldnt say never — they should still have the ability to input
	// someone new but it will be VERY unlikely"). What it changes is
	// PROMINENCE and the reading of a record: at the entrance, enrolling
	// people is an everyday function; downstream of it, an unrecognised name
	// overwhelmingly means the link is behind rather than that somebody was
	// never enrolled.
	//
	// OPTIONAL, and its absence is honest rather than assumed: left unset
	// (the zero value, which every existing deploy config has), no post is
	// treated as the entrance and no post is treated as downstream — the
	// intake screen reads neutrally and claims nothing. It is deliberately
	// NOT inferred from the peer or destination lists, which are symmetric
	// and cannot distinguish an entrance from a pad.
	EntranceSiteID string `toml:"entrance_site_id"`

	// Orientation says whether THIS post administers a site orientation
	// (D-88, 2026-09-05: every site except the ice road has its own; taken
	// AT that site; once per person per site, forever) — and therefore
	// whether anyone owes one before this site enters them (D-91). `true`
	// at a work site (a pad): the marking control is registered, and the
	// arrival path will (step 7) hold un-oriented occupants as not entered.
	// `false` at a road post: nobody owes an orientation here, and the
	// marking route does not exist (not registered, never "disabled" — the
	// dev_routes pattern).
	//
	// ⚠ REQUIRED, WITH NO DEFAULT — refused at startup when absent, the
	// D-77 pattern validate() applies to [[destinations]]. It is a *bool
	// precisely so that ABSENT and `false` are distinguishable: a plain
	// bool reads a forgotten line as `false`, and at a pad that silently
	// enters every un-oriented arrival — the unsafe direction under D-91.
	// A default of `true` is no better: it offers Ice Road a control that
	// means nothing there. "Unset means the application's default applies,
	// not that the feature is off" — so there is no default. Read it with
	// HasOrientation().
	//
	// ⚠ ON THIS SITE'S OWN FILE, never on [[destinations]] or [[peers]]:
	// the exemption is a property of a SITE, and no line here names another
	// site (D-77: no pair encoded, no site identity in source). A post
	// therefore cannot know whether a PEER has an orientation; that read
	// was withdrawn (schema-10 §5C.7) and nothing here revives it.
	Orientation *bool `toml:"orientation"`

	// SiteType is the owner's per-site kind (R-4/R-5, 2026-09-09): whether
	// people sign in and out of THIS site per visit ("text_message" — the
	// owner's text-message sites, and any gate post that enters each visit
	// by hand) or live on it for weeks at a time ("camp"). Read by exactly
	// one consumer, the visit report (domain.VisitReport): at a text-message
	// site a visit closed by the person's own return with no departure
	// recorded is a MISSED SIGN-OUT; at a camp the same shape is normal —
	// "the persons staying in the camp may be on site for a month or more
	// regularly" — and is never a failure. Nothing on the muster, the gate
	// or any printed sheet reads it: D-13 keeps holding wherever an officer
	// looks.
	//
	// ⚠ REQUIRED, WITH NO DEFAULT — refused at startup when absent or
	// misspelt, the same D-77 point as `orientation` above and for the same
	// reason: a forgotten line that defaulted to "camp" would silently stop
	// tracking failures at a text-message site (the unsafe direction under
	// R-4), and one that defaulted to "text_message" would report every camp
	// resident's month as a failure (what R-5 forbids). The vocabulary is
	// domain.ParseSiteType's, checked in validate().
	//
	// ⚠ ON THIS SITE'S OWN FILE ONLY, like `orientation`: config is
	// local-only (design §7), so a post knows its own type and no peer's,
	// and the report says so for every peer site it prints.
	SiteType domain.SiteType `toml:"site_type"`

	// ShiftStarts (FIX16 G) is OPTIONAL: the local-clock "HH:MM" times a
	// shift begins, for the gate's Shift totals panel — `shift_starts =
	// ["06:00", "18:00"]`. Absent means 06:00 and 18:00 (the panel's hover
	// text says so). A key that is absent must keep an old config starting.
	ShiftStarts []string `toml:"shift_starts"`

	// ThemeCookieDomain is OPTIONAL. The light/dark switch (web/theme.go)
	// remembers its choice in a cookie; unset (the production default) that
	// cookie is HOST-ONLY, so each post remembers its own choice. Set it to a
	// parent domain every post is served under (the demo: ".jnkfacey.com",
	// where each post is its own sc-<site> subdomain) and the cookie carries
	// Domain=<it>, so one choice applies at every post. Config only — the
	// cookie's scope is never taken from a request. validate() refuses
	// anything that is not a plain registrable-looking domain name (see
	// validThemeCookieDomain); a value a browser would silently drop must
	// not start up looking configured.
	ThemeCookieDomain string `toml:"theme_cookie_domain"`

	// FIX16 D (James, 2026-10-06): the nightly local backup. All three keys
	// are OPTIONAL — an existing toml without them starts unchanged and
	// takes the defaults (read them through BackupDirPath / BackupMirrorPath
	// / BackupAt, never the raw strings).
	//
	// BackupDir is where the verified copies are kept (the newest 7). Unset:
	// a "backups" folder beside db_path. A relative path resolves like
	// db_path does — against the working directory (D-85).
	BackupDir string `toml:"backup_dir"`
	// BackupMirrorDir is an optional SECOND folder — a USB drive — that each
	// verified copy is also written to and verified in, keeping its own 7.
	// Unset: no second copy. A failed mirror write never fails the primary.
	BackupMirrorDir string `toml:"backup_mirror_dir"`
	// BackupTime is the local time of day the nightly copy is taken, "HH:MM"
	// on the 24-hour clock. Unset: DefaultBackupTime plus this post's own
	// offset (BackupOffsetMinutes) — set it to take the copy at exactly that
	// time.
	BackupTime string `toml:"backup_time"`
}

// DefaultBackupTime is backup_time's default: 03:00 local, the quietest hour.
const DefaultBackupTime = "03:00"

// BackupDirPath is the folder the nightly copies go to: backup_dir, or a
// "backups" folder beside db_path when unset.
func (c *Config) BackupDirPath() string {
	if d := strings.TrimSpace(c.BackupDir); d != "" {
		return d
	}
	return filepath.Join(filepath.Dir(c.DBPath), "backups")
}

// BackupMirrorPath is the optional second folder ("" when unset).
func (c *Config) BackupMirrorPath() string { return strings.TrimSpace(c.BackupMirrorDir) }

// BackupOffsetMinutes is how many minutes after DefaultBackupTime a post
// with no backup_time takes its copy: 0-14, from its site_id (FNV-1a), so
// it is the same every night and on every start. ⟦FIX17b, sc-stress F19⟧
// Several posts sharing one host and disk (CT510, the staging stack) all
// copied at 03:00 together and the gates slowed (mean 2.4 s, max 6.5 s for
// two minutes); offset by site they rarely meet. On separate laptops it
// changes nothing that matters. ⟦was: every unset post at exactly 03:00⟧
func BackupOffsetMinutes(siteID string) int {
	h := fnv.New32a()
	h.Write([]byte(siteID))
	return int(h.Sum32() % 15)
}

// BackupAt is backup_time as hour and minute. An explicit backup_time is
// taken exactly; unset, it is DefaultBackupTime plus BackupOffsetMinutes.
// validate() has already refused a malformed value on a loaded config.
func (c *Config) BackupAt() (hour, minute int) {
	if strings.TrimSpace(c.BackupTime) != "" {
		if h, m, err := parseClock(c.BackupTime); err == nil {
			return h, m
		}
	}
	h, m, _ := parseClock(DefaultBackupTime)
	t := h*60 + m + BackupOffsetMinutes(c.SiteID)
	return (t / 60) % 24, t % 60
}

// parseClock reads "HH:MM" (24-hour). Empty means the default.
func parseClock(s string) (int, int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		s = DefaultBackupTime
	}
	var h, m int
	if n, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil || n != 2 || len(s) != 5 || s[2] != ':' {
		return 0, 0, fmt.Errorf("%q is not a time of day — write it as \"HH:MM\" on the 24-hour clock, e.g. \"03:00\"", s)
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("%q is not a time of day — hours 00-23, minutes 00-59", s)
	}
	return h, m, nil
}

// ThemeCookieDomainName is ThemeCookieDomain without its optional leading
// dot, lower-cased — the form a request host is compared against. "" when
// unset.
func (c *Config) ThemeCookieDomainName() string {
	return strings.ToLower(strings.TrimPrefix(c.ThemeCookieDomain, "."))
}

// ShiftStartMinutes is ShiftStarts as minutes after local midnight, ascending;
// the 06:00 / 18:00 default when none is configured. configured reports
// whether the config named them (the panel's hover text says which).
func (c *Config) ShiftStartMinutes() (mins []int, configured bool) {
	if len(c.ShiftStarts) == 0 {
		return append([]int(nil), domain.DefaultShiftStarts...), false
	}
	for _, s := range c.ShiftStarts {
		if m, err := domain.ParseShiftStart(s); err == nil {
			mins = append(mins, m)
		}
	}
	sort.Ints(mins)
	return mins, true
}

// HasOrientation reports whether this post administers a site orientation
// (D-88). A loaded config always has the key (validate() refuses its
// absence); a Config literal built without it reads as false.
func (c *Config) HasOrientation() bool { return c.Orientation != nil && *c.Orientation }

// VehicleModeEnabled ⟦R-28, team-lead, 2026-09-16: "enable vehicle mode
// where the site has >=1 configured destination (the linked posts), rather
// than adding a config key"⟧ derives the answer from DestinationList()
// rather than a new TOML key — the same list the dispatch UI, the
// destination select and ControlledDestinationIDs already read, so a site
// that can dispatch or receive a trip is, by that same fact, a site where
// checking in a whole vehicle at once makes sense. A site with no
// destinations at all (the common case — most posts are not one half of a
// linked pair) reads false here and never offers the choice. See
// DestinationList's own comment for why an old peers-only config (no
// explicit [[destinations]]) still resolves a non-empty list: that is the
// same "existing configs keep working unchanged" fallback every other
// consumer of this list already relies on, not a new behaviour introduced
// here.
func (c *Config) VehicleModeEnabled() bool { return len(c.DestinationList()) > 0 }

// EntranceKnown reports whether this deployment has been told which post is
// the entrance. Separate from IsEntrance on purpose: "this post is not the
// entrance" and "nobody has said which post is the entrance" are different
// statements, and collapsing them would make an unconfigured site render as
// though it were downstream of an entrance it has never heard of.
func (c *Config) EntranceKnown() bool { return c.EntranceSiteID != "" }

// IsEntrance reports whether THIS post is the site's entrance. False when
// unset — always read it with EntranceKnown.
func (c *Config) IsEntrance() bool { return c.EntranceKnown() && c.EntranceSiteID == c.SiteID }

func (c *Config) SyncInterval() time.Duration {
	return time.Duration(c.SyncIntervalSeconds) * time.Second
}

// PostName is the site control post THIS box is — the unit of attribution
// (James, D-5 2026-08-21: "yes by post not person"). There is exactly one
// post per box, so it is configuration, never a session choice and never a
// per-person list: the two posts are Ice Road and Pad Site Control.
//
// It is deliberately derived from DisplayName rather than being a fourth
// name for the same thing, and it falls back to SiteID rather than to "" —
// an attribution that renders blank is worse than one that renders an id,
// and SiteID is the same identity the replication envelope already carries
// structurally (store.Envelope.SiteID/OriginAuth, enforced at ingest).
func (c *Config) PostName() string {
	if c.DisplayName != "" {
		return c.DisplayName
	}
	return SiteLabel(c.SiteID)
}

// KnownSiteTypes is every site type this post has been told: its own
// (required) and each peer's optional site_type (FIX16f). The visit report
// gives a verdict only for a site in this set and names the rest as untyped.
func (c *Config) KnownSiteTypes() domain.SiteTypes {
	out := domain.SiteTypes{c.SiteID: c.SiteType}
	for _, p := range c.Peers {
		if p.SiteType != "" {
			out[p.SiteID] = p.SiteType
		}
	}
	return out
}

// SiteName is the one way a post names ANY site id on a screen: this post's
// own display name, else a configured destination's, else a configured
// peer's display_name, else SiteLabel's fallback. It must not depend on
// Destinations (a post with `destinations = []` still names its peers).
// James, 2026-10-04 (FINDINGS-R4 cluster I): no screen shows a raw site id.
func (c *Config) SiteName(id string) string {
	if c == nil {
		return SiteLabel(id)
	}
	if id == c.SiteID && strings.TrimSpace(c.DisplayName) != "" {
		return strings.TrimSpace(c.DisplayName)
	}
	for _, d := range c.Destinations {
		if d.ID == id && strings.TrimSpace(d.DisplayName) != "" {
			return strings.TrimSpace(d.DisplayName)
		}
	}
	for _, p := range c.Peers {
		if p.SiteID == id && strings.TrimSpace(p.DisplayName) != "" {
			return strings.TrimSpace(p.DisplayName)
		}
	}
	// A retired post (FIX16 H): its records stay, and keep their name.
	for _, p := range c.FormerPeers {
		if p.SiteID == id && strings.TrimSpace(p.DisplayName) != "" {
			return strings.TrimSpace(p.DisplayName)
		}
	}
	return SiteLabel(id)
}

// IsFormerPeer reports whether id is a post retired from this mesh
// ([[former_peers]]). FIX17b (sc-stress F15): history, report and search
// filters still offer it, marked retired; dispatch and gate pickers never do
// (they are built from Peers and Destinations, which it is not in).
func (c *Config) IsFormerPeer(id string) bool {
	if c == nil {
		return false
	}
	for _, p := range c.FormerPeers {
		if p.SiteID == id {
			return true
		}
	}
	return false
}

// SiteLongName is the full name of a site where a long name belongs (top
// bar, site-page heading): this post's LongName, or a peer's long_name.
// Empty when none is configured — callers then show only the short name,
// never a duplicate of it.
func (c *Config) SiteLongName(id string) string {
	if c == nil {
		return ""
	}
	long := ""
	if id == c.SiteID {
		long = strings.TrimSpace(c.LongName)
	} else {
		for _, p := range c.Peers {
			if p.SiteID == id {
				long = strings.TrimSpace(p.LongName)
			}
		}
		for _, p := range c.FormerPeers {
			if p.SiteID == id && long == "" {
				long = strings.TrimSpace(p.LongName)
			}
		}
	}
	if long == c.SiteName(id) {
		return ""
	}
	return long
}

// DestinationList is what the dispatch UI and handler consume: the configured
// destinations with display-name defaulting applied, or — when none are
// configured — one controlled destination per peer (the pre-destinations
// behaviour, so existing configs keep working unchanged). validate()
// guarantees the fallback branch is only ever reached with at most one
// peer (D-77); the loop is kept general rather than indexing [0] so that
// guarantee lives in exactly one place.
func (c *Config) DestinationList() []Destination {
	if len(c.Destinations) == 0 && c.destinationsNone {
		return []Destination{} // declared `destinations = []`: no travel, never derived
	}
	if len(c.Destinations) == 0 {
		out := make([]Destination, 0, len(c.Peers))
		for _, p := range c.Peers {
			out = append(out, Destination{ID: p.SiteID, DisplayName: c.SiteName(p.SiteID), Controlled: true})
		}
		return out
	}
	out := make([]Destination, 0, len(c.Destinations))
	for _, d := range c.Destinations {
		if d.DisplayName == "" {
			d.DisplayName = c.SiteName(d.ID)
		}
		out = append(out, d)
	}
	return out
}

// ControlledDestinationIDs / UncontrolledDestinationIDs split DestinationList
// into the two membership sets domain.Dispatch validates against (F4 and its
// uncontrolled analogue).
func (c *Config) ControlledDestinationIDs() []string {
	out := []string{} // non-nil even when empty: membership is always enforced
	for _, d := range c.DestinationList() {
		if d.Controlled {
			out = append(out, d.ID)
		}
	}
	return out
}

func (c *Config) UncontrolledDestinationIDs() []string {
	out := []string{} // non-nil even when empty: membership is always enforced
	for _, d := range c.DestinationList() {
		if !d.Controlled {
			out = append(out, d.ID)
		}
	}
	return out
}

// TravelMinutesTo returns the configured travel minutes for the road segment
// to destID — the dispatch form's deadline prefill. ok=false when destID is
// not a configured destination or carries no travel time. The same number
// serves both directions of a segment (see Destination.TravelMinutes).
//
// ⟦was: PhasesApply, PhaseInfo, DispatchAllowed, ReviewInTransitFor,
// HighestConfiguredPhase, SegmentMinutes (per-phase minutes with an
// unknown-phase fallback) and PhaseTable — all removed with weather phases,
// James 2026-10-04⟧
func (c *Config) TravelMinutesTo(destID string) (int64, bool) {
	for _, d := range c.Destinations {
		if d.ID == destID && d.TravelMinutes > 0 {
			return d.TravelMinutes, true
		}
	}
	return 0, false
}

// renamedConfigKeys maps a key this program USED to accept to the key that
// replaced it, so a post meeting a stale config says what to do rather than
// what is wrong. ⟦W8, DR-W8-17⟧
var renamedConfigKeys = map[string]string{
	"muster_min_crew_size": "muster_min_group_size",
}

// Load reads, decodes and validates a config file. Unknown keys are an error:
// a typoed key silently reverting a field to its zero value is exactly the
// "unset means the default applies" trap.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return Parse(raw, path)
}

// Parse is Load on bytes already in hand — the SAME decode and the same
// validate(), with `path` used only to name the file in a refusal. It
// exists for `sitectl admin` (FIX16 H), which must prove a config it has
// generated or edited would start BEFORE writing it anywhere: a key-bearing
// file must never be written to a temp path merely to be checked.
func Parse(raw []byte, path string) (*Config, error) {
	if err := refuseRemovedWeatherConfig(string(raw)); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	var c Config
	meta, err := toml.Decode(string(raw), &c)
	if err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	if undec := meta.Undecoded(); len(undec) > 0 {
		key := undec[0].String()
		// ⚠⚠ A RENAMED KEY IS NOT A TYPO, AND SAYING SO SENDS SOMEBODY
		// HUNTING A MIS-SPELLING THAT IS NOT THERE, AT A POST THAT WILL NOT
		// START. `muster_min_crew_size` was renamed at the v10 changeover
		// because the CONCEPT changed — a crew is not a trade — and it is the
		// ONE B-2 REMOVE entry this program enforces at boot. The refusal
		// NAMES THE REPLACEMENT.
		//
		// ⚠ Deleting the line instead of renaming it is the residual this
		// does NOT close: an absent key is legal and gives the zero value,
		// which silently disables muster folding. Named here so it is not
		// discovered later as a defect.
		if r, ok := renamedConfigKeys[key]; ok {
			return nil, fmt.Errorf("config %s: %q was renamed to %q at the v10 changeover — rename the line; the value is unchanged", path, key, r)
		}
		return nil, fmt.Errorf("config %s: unknown key %q (typo?)", path, key)
	}
	c.destinationsNone = meta.IsDefined("destinations") && len(c.Destinations) == 0
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	return &c, nil
}

// refuseRemovedWeatherConfig refuses, LOUDLY and by name, the two config
// shapes weather phases left behind (James, 2026-10-04: "go ahead and delete
// the phase conditions"): any [weather] / [[weather.phase]] table, and a
// travel_minutes written as the old per-phase table. D-77: a stale key must
// refuse, never guess — and these two would not even reach the generic
// unknown-key refusal below with a useful message (the table form of
// travel_minutes fails toml.Decode as a bare type mismatch), so they are
// checked first, on a schema-free decode, with the replacement named.
func refuseRemovedWeatherConfig(raw string) error {
	var generic map[string]any
	if _, err := toml.Decode(raw, &generic); err != nil {
		return nil // not ours to report — the typed decode states the syntax error
	}
	if _, ok := generic["weather"]; ok {
		return fmt.Errorf("[[weather.phase]] / [weather] is no longer a config table — weather phases were removed (James, 2026-10-04: officers do not dispatch in bad weather; that is their call, not the system's). Delete every [[weather.phase]] block. Nothing replaces it")
	}
	dests, _ := generic["destinations"].([]map[string]any)
	for i, d := range dests {
		if tm, ok := d["travel_minutes"]; ok {
			if _, isTable := tm.(map[string]any); isTable {
				return fmt.Errorf("destinations[%d] (%v): travel_minutes is a per-phase table, which was removed with weather phases (James, 2026-10-04) — write ONE number of minutes for the segment, e.g. travel_minutes = 65 (the old phase-0 entry)", i, d["id"])
			}
		}
	}
	return nil
}

func (c *Config) validate() error {
	switch {
	case c.SiteID == "":
		return fmt.Errorf("site_id is required")
	case c.ListenAddr == "":
		return fmt.Errorf("listen_addr is required")
	case c.NodeEpoch < 1:
		return fmt.Errorf("node_epoch must be >= 1 (seeded at provisioning, never inferred)")
	case c.DBPath == "":
		return fmt.Errorf("db_path is required")
	case c.SyncIntervalSeconds <= 0:
		return fmt.Errorf("sync_interval_seconds must be > 0")
	case c.MusterMinGroupSize < 0:
		return fmt.Errorf("muster_min_group_size must be >= 0")
	case c.LoginPassword == "":
		return fmt.Errorf("login_password is required (design §12/C11: per-site login)")
	case c.ThemeCookieDomain != "" && !validThemeCookieDomain(c.ThemeCookieDomain):
		return fmt.Errorf("theme_cookie_domain %q is not a usable cookie domain: write a plain domain name with at least two labels and no scheme, port, path or IP address (e.g. \".example.com\"), or delete the line to keep the theme cookie host-only", c.ThemeCookieDomain)
	case c.ProxyAuthToken != "" && !listenIsLoopback(c.ListenAddr):
		return fmt.Errorf("proxy_auth_token requires a loopback listen_addr (127.0.0.1/::1/localhost), got %q: the header is only trustable behind the local proxy", c.ListenAddr)
	}
	// D-88 / D-91 (2026-09-05): the per-site orientation key is REQUIRED and
	// has NO DEFAULT — the same fail-loudly point, and the same reasoning,
	// as the D-77 destinations refusal below. A forgotten line at a pad
	// would otherwise read as `false` and silently enter every un-oriented
	// arrival; a forgotten line at a road post read as `true` would offer a
	// control that means nothing there. Either default is a silent
	// per-topology guess, which is the shape D-77 rules against. The
	// refusal names the key, both legal values and what each means.
	if c.Orientation == nil {
		return fmt.Errorf("orientation: the key is required and has no default — write `orientation = true` if people take THIS site's orientation at this post (a work site: nobody is entered here until they complete it, D-91), or `orientation = false` if nobody owes an orientation here (a road post — the ice road, D-88); a missing key must refuse rather than guess (D-77)")
	}
	// R-4 / R-5 (2026-09-09): the per-site type is REQUIRED with NO DEFAULT
	// — the orientation/D-77 pattern again. Absent and misspelt are two
	// different refusals: the first names the line to write, the second
	// names the value that is wrong and the two that are legal.
	if c.SiteType == "" {
		return fmt.Errorf("site_type: the key is required and has no default — write `site_type = \"text_message\"` if people sign in and out of THIS site per visit (the visit report then counts a visit closed by the person's own return, with no departure recorded, as a missed sign-out — R-4), or `site_type = \"camp\"` if people live on THIS site for weeks at a time (the same shape is normal there and is never reported as a failure — R-5); a missing key must refuse rather than guess (D-77)")
	}
	if _, err := domain.ParseSiteType(string(c.SiteType)); err != nil {
		return fmt.Errorf("site_type: %w", err)
	}
	if len(c.ShiftStarts) > 0 {
		seenShift := map[int]bool{}
		for i, v := range c.ShiftStarts {
			m, err := domain.ParseShiftStart(v)
			if err != nil {
				return fmt.Errorf("shift_starts[%d]: %w", i, err)
			}
			if seenShift[m] {
				return fmt.Errorf("shift_starts[%d]: %q is listed twice", i, v)
			}
			seenShift[m] = true
		}
	}
	seen := map[string]bool{}
	for i, p := range c.Peers {
		if p.SiteID == "" || p.BaseURL == "" {
			return fmt.Errorf("peers[%d]: site_id and base_url are required", i)
		}
		if p.SiteID == c.SiteID {
			return fmt.Errorf("peers[%d]: a site cannot list itself as a peer", i)
		}
		if seen[p.SiteID] {
			return fmt.Errorf("peers[%d]: duplicate peer site_id %q", i, p.SiteID)
		}
		if p.SiteType != "" {
			if _, err := domain.ParseSiteType(string(p.SiteType)); err != nil {
				return fmt.Errorf("peers[%d] (%s): site_type: %w", i, p.SiteID, err)
			}
		}
		seen[p.SiteID] = true
	}
	// [[former_peers]] (FIX16 H): a retired post is display-only. It must
	// not also be a live peer (the two lists would disagree about whether
	// it is pulled from), nor this post itself, nor listed twice.
	seenFormer := map[string]bool{}
	for i, p := range c.FormerPeers {
		switch {
		case p.SiteID == "":
			return fmt.Errorf("former_peers[%d]: site_id is required", i)
		case p.SiteID == c.SiteID:
			return fmt.Errorf("former_peers[%d]: a post cannot list itself as a former peer", i)
		case seen[p.SiteID]:
			return fmt.Errorf("former_peers[%d]: %q is still a configured peer — a post is either live in [[peers]] or retired in [[former_peers]], never both", i, p.SiteID)
		case seenFormer[p.SiteID]:
			return fmt.Errorf("former_peers[%d]: duplicate former peer site_id %q", i, p.SiteID)
		}
		seenFormer[p.SiteID] = true
	}
	// D-77 (2026-09-05): with MORE THAN ONE peer, [[destinations]] must be
	// EXPLICIT. DestinationList's fallback synthesises one CONTROLLED
	// destination per peer — invisibly correct at two posts, where "every
	// peer" and "the one road" are the same set, and wrong the moment a
	// third site exists: each synthesised entry arms an in-transit timer and
	// an overdue alert for a trip on a road this post may have no way to
	// drive. Peers say who this post REPLICATES with; destinations say who
	// it can DISPATCH to; only at one peer may the second be read off the
	// first. A silent default that is correct for exactly the current
	// topology is the shape D-77 rules against, so refuse here — the same
	// fail-loudly point as the rest of this function — naming what to write.
	// Deliberately NOT narrowed to any named pair of sites: no site identity
	// belongs in source (D-77's standing test).
	if len(c.Peers) > 1 && len(c.Destinations) == 0 && !c.destinationsNone {
		return fmt.Errorf("destinations: %d peers are configured but no [[destinations]] block is (write `destinations = []` for a post with no travel) — with more than one peer the dispatch list must be written explicitly, because deriving it from the peer list would arm an in-transit deadline toward every peer, including sites this post has no road to (D-77); add one [[destinations]] entry per site this post actually dispatches to (controlled = true for a peer with a gate, controlled = false for an ungated pad)", len(c.Peers))
	}
	// Destination ↔ peer reconciliation, at STARTUP — not at dispatch time.
	// The destination decides which site can close the expectation, so both
	// mismatches are misconfigurations that must fail loudly here:
	//   - a CONTROLLED destination that is not a configured peer promises a
	//     deadline nobody can close (F4's exact hazard, moved into config);
	//   - an UNCONTROLLED destination that IS a configured peer would
	//     silently strip the timer from runs to a site that has a gate and
	//     could have confirmed the arrival.
	seenDest := map[string]bool{}
	for i, d := range c.Destinations {
		switch {
		case d.ID == "":
			return fmt.Errorf("destinations[%d]: id is required", i)
		case d.ID == RoadWorkChoice:
			// James, 2026-10-03: the dispatch forms submit this id for
			// "working on the road"; a real destination spelled the same
			// way would be unreachable from every form and ambiguous on a
			// raw POST. Refused at the established fail-loudly point.
			return fmt.Errorf("destinations[%d]: id %q is reserved for the working-on-the-road choice ([road_work]); give this destination another id", i, d.ID)
		case d.ID == c.SiteID:
			return fmt.Errorf("destinations[%d]: a site cannot be its own destination", i)
		case seenDest[d.ID]:
			return fmt.Errorf("destinations[%d]: duplicate destination id %q", i, d.ID)
		case d.Controlled && !seen[d.ID]:
			return fmt.Errorf("destinations[%d]: controlled destination %q is not a configured peer — no site could ever close its expectations; add it to [[peers]] or mark it controlled = false only if it truly has no gate", i, d.ID)
		case !d.Controlled && seen[d.ID]:
			return fmt.Errorf("destinations[%d]: uncontrolled destination %q IS a configured peer — a peer has a gate that can confirm arrivals; marking it uncontrolled would silently strip the timer from every run there", i, d.ID)
		}
		seenDest[d.ID] = true
	}
	// [road_work] (James, 2026-10-03): a negative limit is a typo, never a
	// shorter shift — refused. 0/omitted takes the default (twelve hours);
	// RoadWorkLimitMinutes applies it, so the file need only say the table
	// exists for the category to be offered with the standard limit.
	if c.RoadWork != nil && c.RoadWork.LimitMinutes < 0 {
		return fmt.Errorf("road_work: limit_minutes must be a positive number of minutes (omit it for the default, %d = twelve hours), got %d", DefaultRoadWorkLimitMinutes, c.RoadWork.LimitMinutes)
	}
	// travel_minutes (one number per segment since weather phases were
	// removed, James 2026-10-04): a negative time is a typo, refused loudly.
	// ⟦was: the [[weather.phase]] uniqueness checks and the per-phase table
	// checks (a "0" entry, every declared phase covered, a decreasing-time
	// WARNING)⟧
	if _, _, err := parseClock(c.BackupTime); err != nil {
		return fmt.Errorf("backup_time: %w (or delete the line for the default, %s)", err, DefaultBackupTime)
	}
	for i, d := range c.Destinations {
		if d.TravelMinutes < 0 {
			return fmt.Errorf("destinations[%d] (%s): travel_minutes must be a positive number of minutes (omit it for no prefill), got %d", i, d.ID, d.TravelMinutes)
		}
	}
	return nil
}

// listenIsLoopback reports whether addr (host:port) binds only the loopback
// interface. An empty or wildcard host binds every interface and is refused.
// validThemeCookieDomain admits a lower-case DNS name with an optional
// single leading dot and at least two labels ("example.com", never "com"),
// each label 1-63 of [a-z0-9-] not starting or ending with "-", and a last
// label that is not all digits (so an IP address is refused: a Domain
// attribute on an IP is dropped by browsers). Deliberately strict — upper
// case, a scheme, a port, a path, whitespace or a wildcard are refused
// rather than normalised, so what is written is exactly what is sent.
// It cannot consult the public-suffix list (no dependency for it); a
// two-label public suffix such as "co.uk" would pass here and simply be
// refused by the browser, leaving the cookie unset rather than too broad.
func validThemeCookieDomain(d string) bool {
	d = strings.TrimPrefix(d, ".")
	if d == "" || len(d) > 253 {
		return false
	}
	labels := strings.Split(d, ".")
	if len(labels) < 2 {
		return false
	}
	for _, l := range labels {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for _, r := range l {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	last := labels[len(labels)-1]
	if strings.Trim(last, "0123456789") == "" {
		return false
	}
	return true
}

func listenIsLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// SiteLabel renders a site id for a person to read; see domain.SiteLabel.
func SiteLabel(id string) string { return domain.SiteLabel(id) }
