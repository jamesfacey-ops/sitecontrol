// refdata.go — company/trade/person as authored events + folds (design
// §8.8-ii, chunk-plan C5). All three are the reference-data replication
// class: open sets, any-site-authored, site-prefixed IDs (design §7). Company
// and person have NO uniqueness check at creation — duplicates are the
// design's own worked example (§8.7 case 2: "Kakivik" minted at two posts,
// both rows survive, no arbitration, the merge snapshot groups them once
// linked).
//
// W8 (item 10) REPLACED this file's third reference entity with the TRADE
// register. It is a FRESH entity, never a re-purpose (D-26 part 2), and three
// things about it differ from its predecessor and are stated here because a
// reader who remembers the old shape will otherwise assume them:
//
//   - a trade is NOT scoped to a company ⟦DR-W8-2⟧, so its uniqueness
//     invariant is SITE-WIDE on name ⟦DR-W8-11⟧ — the company predicate is
//     gone, not weakened;
//   - a trade carries three POSITIONAL handset numbers (day/night/shared)
//     which arrive only by amendment, never at creation ⟦DR-W8-4⟧;
//   - crew MEMBERSHIP is not modelled and may not be added back under
//     another name (D-23). The reference direction is person.trade_id and
//     only that.
package domain

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"sitecontrol/internal/store"
)

// --- event payloads ---

type CompanyCreated struct {
	Type       string  `json:"type"` // "company_created"
	CompanyID  string  `json:"company_id"`
	Name       string  `json:"name"`
	RecordedBy *string `json:"recorded_by,omitempty"` // F2: session officer, or nil if none picked (§10's nullable contract)
}

// TradeCreated is the created-state trade record (W8, item 10). Its keys are
// EXACTLY `trade_id`, `name` and `recorded_by` — gate B-2's ADD rows.
//
// ⚠ NO `company_id` ⟦DR-W8-2⟧: a trade is a site-wide open set and is not
// scoped to a company. The predecessor's company predicate is DELETED, not
// carried, and its absence is asserted (W8-2(a)).
//
// ⚠ NO phone keys: the three positional handset numbers are AMENDMENT facts
// (trade_amend.go). trade_created DOES carry `recorded_by` while
// trade_amended deliberately does not ⟦DR-W8-5⟧ — the two shapes differ on
// purpose, so a scan forbidding `recorded_by` "everywhere on trade" is wrong
// on the created half.
type TradeCreated struct {
	Type       string  `json:"type"` // "trade_created"
	TradeID    string  `json:"trade_id"`
	Name       string  `json:"name"`
	RecordedBy *string `json:"recorded_by,omitempty"` // F2
}

// PersonCreated carries the created-state person facts. The three optional
// name/position facts (W5a, item 1 + D-21.4) were added here because W3 had
// already landed them on PersonAmended and the projection, leaving creation
// as the only door that threw them away.
//
// D-48 governs what they mean: `Name` is AUTHORITATIVE and the parts are
// extras that MAY diverge from it — lawfully, silently, with nothing
// detecting, warning about or repairing the disagreement. So:
//   - nothing composes Name from the parts, anywhere, ever (W5a-3/W5a-8);
//   - no read path consults a part for anything Name answers — identity
//     matching, the roster, the muster, the gate's exact-match resolver;
//   - the person-name sorts read Name, NEVER LastName. Keying a sort on
//     LastName once the column exists is the plausible "improvement" and it
//     orders the roster by an invisible field that may disagree with the
//     printed page (D-48 derived ruling 2).
//
// nil means ABSENT (never asserted), which is a different thing from
// empty-string (W3-3) — hence `omitempty`, so a partless person_created
// carries no part keys into its payload bytes and therefore none into its
// row hash.
type PersonCreated struct {
	Type      string  `json:"type"` // "person_created"
	PersonID  string  `json:"person_id"`
	Name      string  `json:"name"`
	CompanyID *string `json:"company_id,omitempty"` // roster DEFAULT only (§8.8-i); never rewritten to fix history
	// TradeID is the roster DEFAULT only. ⚠ W8 (D-66) puts a person's trade
	// in the CREATION record — "how most persons enter the roster" — so
	// `trade_id` is a PersonCreated payload key and therefore A KEY INSIDE
	// HASHED BYTES (gate B-2). It REPLACES the predecessor entity's key,
	// which is struck from the wire in the same chunk.
	// ⚠ NO `omitempty`, AND THAT IS THE CRITERION RATHER THAN A STYLE CHOICE.
	// W8-23(a) asserts the MARSHALLED BYTES of a real authored person_created
	// CARRY THE LITERAL KEY `trade_id`, and gate B-2 freezes it as a
	// PersonCreated payload key — a key INSIDE HASHED BYTES. With `omitempty`
	// the commonest person of all (one enrolled with no trade on file) emits
	// no key at all, so the frozen key would be present on some envelopes and
	// absent from others, and the freeze would mean nothing. A person with no
	// trade emits `"trade_id":null`, which is the wire's own spelling of
	// ABSENT and is a different thing from empty-string.
	TradeID    *string `json:"trade_id"`
	RecordedBy *string `json:"recorded_by,omitempty"` // F2: this is who ADDED the person, not a roster field — never touched by later edits to company/trade defaults
	// The optional name/position facts (W5a). Same spellings W3 landed on
	// PersonAmended (person_amend.go) for the same three facts.
	LastName  *string `json:"last_name,omitempty"`
	FirstName *string `json:"first_name,omitempty"`
	Position  *string `json:"position,omitempty"` // D-21.4: nullable, FREE TEXT — no enum, no validation against a list, no branch on its content
	// Phone (W5b, item 2), same spelling W3 landed on PersonAmended.
	//
	// NULLABLE AND MANDATORY ARE NOT IN TENSION, and the distinction is the
	// whole of this field's design. Nullable is a STORAGE property and lives
	// here; mandatory is an EDGE property and lives in exactly one handler
	// (web/intake.go's handleIntake). Refusing a phoneless person_created at
	// the fold would fork the two posts' logs permanently (W3-2), so this
	// field must be omissible however it arrived.
	//
	// D-52 is why "mandatory" is a property of ONE HANDLER and not of this
	// event: person_created is authored locally from TWO doors — intake, and
	// add-on-demand at the gate (checkin.go's resolvePersonSlot, "the
	// creation site that matters most … how most persons enter the roster").
	// James ruled the gate door EXEMPT: an officer with a person standing in
	// front of them is not sent to find a phone number. So NOTHING here, in
	// createPerson, or in the fold may require it — a check in any of those
	// places passes every intake-only test and breaks the busiest operational
	// path in the system.
	//
	// The honest consequence, recorded rather than smoothed over (D-52's own
	// instruction): "phone is enrolment-mandatory" is true of INTAKE and
	// FALSE OF THE SYSTEM. A muster read for contact purposes will have gaps,
	// and they cluster on the people who arrived at a gate. The amendment
	// edge also still accepts Clear:["phone"] (W3, shipped; the retrofit is
	// OWED and unowned — post-lock amendments §3), so a phone recorded at
	// intake can be removed afterwards.
	//
	// The value is stored VERBATIM (plan `:2037`): no digit-stripping, no
	// space/dash/paren removal, no country code prepended, no truncation, no
	// case-fold, at any authoring or fold path. A number an operator can read
	// back to the person in front of them beats a normalised one.
	Phone *string `json:"phone,omitempty"`
	// ⟦SchemaVersion 11⟧ `PadOriented` (W5c, item 7; D-10/D-21.1) is
	// STRUCK from this payload, not migrated ([[D-88]] + ERRATA, [[D-91]]).
	// It was a single flag on the ENROLLING site's person row, named after
	// one site; per-site orientation ("once per person, per site, forever,
	// taken AT that site") makes that the wrong row entirely — under the
	// D-87 choke point only the enroller could ever have recorded a Pad
	// orientation. Orientation is re-built in a later stage of this same
	// increment as the ORIENTING post's own record. Nothing was ever
	// recorded under the old key on a real post ([[D-29]], [[D-92]]).
	// ⟦was: `PadOriented *string `json:"pad_oriented,omitempty"`` and the
	// DR-8 type/value-domain paragraph; D-88 reverses D-10 and D-21.1⟧
}

// --- projection row shapes (mirror the Schema below; used by dumpSection) ---

// Company is the COMPOSED company as of W5d (item 12): the created-state
// projection row plus every company_amended assertion folded over it in
// ascending (site_id, node_epoch, seq) order — Companies()/CompanyByID()
// below return this composed shape, never the raw row (W5d-3).
//
// Name stays a BARE STRING, unlike Person's amendment-borne fields:
// company.name is TEXT NOT NULL and CompanyCreated.Name is a bare string,
// so the composed shape matches. A company whose name was cleared FROM THE
// WIRE (the edge refuses that; the fold never refuses) composes with Name
// == "" — the only representable absent on a non-pointer field — and the
// live muster renders its raw company id (W5d-22(d)).
type Company struct {
	CompanyID  string  `json:"company_id"`
	SiteID     string  `json:"site_id"`
	Name       string  `json:"name"`
	RecordedBy *string `json:"recorded_by"` // F2
	// SameAs is the merge pointer (P-3 option 1a): the composed effective
	// same_as assertion, or nil when un-merged. "Merged?" is answered by
	// THIS field and nothing else; resolution surfaces derive from it per
	// read (LoadCompanyMergeMap). nil means ABSENT, which is a different
	// thing from empty-string (W5d-18) — the distinction is load-bearing.
	SameAs *string `json:"same_as,omitempty"`
}

// Trade is the COMPOSED trade (W8-3): the created-state projection row with
// every `trade_amended` assertion folded over it in ascending
// (site_id, node_epoch, seq) order — Trades()/TradeByID() return this shape,
// never the raw row.
//
// The three handset numbers are POSITIONAL, not personal: they belong to the
// trade's day shift, night shift and shared handset, and they arrive only by
// amendment. `positional(row, shift) = coalesce(phone_<shift>, phone_shared)`
// is stated once and implemented once — TradePhone in trade_amend.go, and
// NOWHERE ELSE (W8-4(a)).
//
// ⚠ THERE IS NO `retired` AND NO `kind` (D-30(b), ⟦DR-W8-3⟧), and no
// membership column of any spelling (D-23, verbatim: "Crew membership is NOT
// modelled. Do not add it back under another name.").
type Trade struct {
	TradeID    string  `json:"trade_id"`
	SiteID     string  `json:"site_id"`
	Name       string  `json:"name"`
	RecordedBy *string `json:"recorded_by"` // F2
	// The amendment-borne facts (trade_amend.go). nil means ABSENT — never
	// asserted, or cleared — which is a different thing from empty-string.
	PhoneDay    *string `json:"phone_day,omitempty"`
	PhoneNight  *string `json:"phone_night,omitempty"`
	PhoneShared *string `json:"phone_shared,omitempty"`
	// SameAs is the merge pointer (P-3 option 1a), the trade analogue of
	// Company.SameAs. A merged-away trade's ROW SURVIVES — `same_as` is a
	// per-field assertion, never a deletion — and resolution happens at the
	// READ (W8-19), not at the authoring edge.
	SameAs *string `json:"same_as,omitempty"`
}

// Person is the COMPOSED person as of W3 (item 4): the created-state
// projection row plus every person_amended assertion folded over it in
// ascending (site_id, node_epoch, seq) order — Persons()/PersonByID()
// below return this composed shape, never the raw row. The pointer fields
// from Phone down exist only through amendments: nil means ABSENT (never
// asserted, or cleared), which is a different thing from empty-string
// (W3-3) — the distinction is load-bearing, not style.
//
// W5a (item 1) moved THREE of them off that list: last_name, first_name and
// position now have a created-state home as well as an amendment one, so a
// non-nil value here may have arrived at creation, by amendment, or both
// (the amendment wins, per the ascending-key fold). W5b (item 2) moved a
// FOURTH the same way: phone. Nothing about the
// composition changes: `Name` stays the answer to "what is this person
// called" and is never recomputed from the parts, however loudly they
// disagree (D-48).
type Person struct {
	PersonID   string  `json:"person_id"`
	SiteID     string  `json:"site_id"`
	Name       string  `json:"name"`
	CompanyID  *string `json:"company_id"`
	RecordedBy *string `json:"recorded_by"` // F2
	// The amendment-borne facts (person_amend.go; CONSOLIDATED §2 item 4).
	// LastName/FirstName/Position are ALSO created-state facts as of W5a,
	// and Phone is as of W5b.
	LastName  *string `json:"last_name,omitempty"`
	FirstName *string `json:"first_name,omitempty"`
	Phone     *string `json:"phone,omitempty"`
	TradeID   *string `json:"trade_id,omitempty"`
	Role      *string `json:"role,omitempty"`
	// ⟦SchemaVersion 11⟧ PadOriented is struck (see PersonCreated).
	Position *string `json:"position,omitempty"`
	Retired  *string `json:"retired,omitempty"`
	// SameAs is the merge pointer (P-3 option 1a): the composed effective
	// same_as assertion, or nil when un-merged. "Merged?" is answered by
	// THIS field and nothing else; resolution surfaces derive from it per
	// read (LoadPersonMergeMap).
	SameAs *string `json:"same_as,omitempty"`
}

// --- projections ---

func companyProjection() store.Projection {
	return store.Projection{
		Name:    "company",
		Table:   "company",
		OrderBy: "company_id",
		// F2: recorded_by added alongside the rest of the shape — this is a
		// PROJECTION SHAPE CHANGE (store.SchemaVersion's own rule, store.go:
		// "IF NOT EXISTS cannot heal a changed shape"). A stored database from
		// before this change has no such column; opening it and authoring a
		// new company would fail at INSERT time with "no such column", not at
		// Open. See the fix report for the required SchemaVersion bump.
		Schema: `CREATE TABLE IF NOT EXISTS company (
			company_id  TEXT PRIMARY KEY,
			site_id     TEXT NOT NULL,
			name        TEXT NOT NULL,
			recorded_by TEXT
		) STRICT;`,
		Apply: applyCompanyCreated,
	}
}

func applyCompanyCreated(tx *sql.Tx, env store.Envelope) error {
	var p CompanyCreated
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return fmt.Errorf("domain: company fold: %w", err)
	}
	if p.Type != "company_created" {
		return nil
	}
	// Plain INSERT, no ON CONFLICT: company_id is site-prefixed and minted
	// fresh per creation, so a genuine PK collision here would mean a broken
	// ID generator, not a legitimate duplicate — duplicates are a NAME match
	// with DISTINCT ids (§8.7 case 2), which is not a conflict at all.
	_, err := tx.Exec(`INSERT INTO company (company_id, site_id, name, recorded_by) VALUES (?, ?, ?, ?)`,
		p.CompanyID, env.SiteID, p.Name, p.RecordedBy)
	if err != nil {
		return fmt.Errorf("domain: company fold: insert %s: %w", p.CompanyID, err)
	}
	return nil
}

// tradeSchemaSQL is the `trade` projection's schema, DR-W8-4's exact seven
// columns.
//
// ⚠⚠ IT IS A PACKAGE-LEVEL CONST AND NOT A LITERAL INSIDE tradeProjection(),
// AND THE PLACEMENT IS LOAD-BEARING RATHER THAN STYLISTIC. W8-4(a) counts
// every FUNCTION BODY that names a shift slot AND the shared slot as a
// POSITIONAL RESOLVER, and requires EXACTLY ONE. A schema literal inside the
// projection function would be a second selection site by that census's own
// definition, and the census is right to say so: a function that can see all
// three slots is a function that can silently choose between them.
//
// Deliberately NO UNIQUE(name) constraint at the DB/fold level. Site-wide
// uniqueness ⟦DR-W8-11⟧ is enforced at AUTHORING time by CreateTrade against
// locally-known state — a business-rule refusal, not arbitration. A DB-level
// UNIQUE plus an ON CONFLICT fold would make WHICH ROW SURVIVES depend on
// delivery ORDER, which store.Projection's contract forbids (folds must be
// order-independent ACROSS streams). So the fold always inserts, and a
// genuine cross-site race coexists exactly as a duplicate company does — and
// is resolved the same way, by an authored `same_as`.
//
// ⚠ THE PHONE COLUMNS ARE NEVER WRITTEN BY THIS FOLD. trade_created carries
// no phone keys; the three positional numbers are amendment facts composed at
// read. The columns exist here because DR-W8-4 pins the created-state shape
// as seven, and because composing into a shape the base table cannot hold is
// how a projection and its composed read drift apart.
const tradeSchemaSQL = `CREATE TABLE IF NOT EXISTS trade (
			trade_id     TEXT PRIMARY KEY,
			site_id      TEXT NOT NULL,
			name         TEXT NOT NULL,
			recorded_by  TEXT,
			phone_day    TEXT,
			phone_night  TEXT,
			phone_shared TEXT
		) STRICT;`

// tradeInsertSQL is applyTradeCreated's INSERT. Package-level for the same
// reason tradeSchemaSQL is: it names no phone column, but keeping the pair
// together is what makes the placement rule visible at the point of use
// rather than a note somebody has to remember.
const tradeInsertSQL = `INSERT INTO trade (trade_id, site_id, name, recorded_by) VALUES (?, ?, ?, ?)`

func tradeProjection() store.Projection {
	return store.Projection{
		Name:    "trade",
		Table:   "trade",
		OrderBy: "trade_id",
		Schema:  tradeSchemaSQL,
		Apply:   applyTradeCreated,
	}
}

func applyTradeCreated(tx *sql.Tx, env store.Envelope) error {
	var p TradeCreated
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return fmt.Errorf("domain: trade fold: %w", err)
	}
	if p.Type != evtTradeCreated {
		return nil
	}
	_, err := tx.Exec(tradeInsertSQL, p.TradeID, env.SiteID, p.Name, p.RecordedBy)
	if err != nil {
		return fmt.Errorf("domain: trade fold: insert %s: %w", p.TradeID, err)
	}
	return nil
}

func personProjection() store.Projection {
	return store.Projection{
		Name:    "person",
		Table:   "person",
		OrderBy: "person_id",
		// F2: recorded_by column added — same shape-change/SchemaVersion note
		// as company above. This is the site officer who ADDED the person,
		// most often at the gate via add-on-demand (checkin.go's
		// resolvePersonSlot) — the row a muster/roster challenge is most
		// likely to ask about.
		//
		// W5a (item 1 + D-21.4) added last_name/first_name/position: nullable
		// TEXT, no default, declared INSIDE this one CREATE literal (W5-X1 —
		// never ALTER TABLE, never DDL concatenated onto a prefix), STRICT
		// preserved. That is a projection SHAPE CHANGE, so store.SchemaVersion
		// governs, by the very rule stated three lines above at :100–105:
		// "IF NOT EXISTS cannot heal a changed shape". SchemaVersion STAYED 10
		// through the schema-10 window (it is 11 since the D-92/D-88/D-91/D-98
		// increment — store.go's bump log): W5a RODE W1's 9 → 10 bump, exactly as trip.cargo_note (item 3, W5e)
		// does — W5e landed no bump. Within-window bumping was ruled WRONG by
		// reports/W5-SCHEMAVERSION-ADJUDICATION.md (2026-09-01), which STRUCK
		// W5-X5's "once per landed shape change, not once per sub-chunk"
		// sentence and post-lock amendment §1 with it; see the bump log in
		// store.go ("NOT bumped for the W5 window's own sub-chunks,
		// deliberately"). The shape rule is about crossing a DEPLOYMENT
		// boundary and this window crosses none, so a store stamped 10 opens
		// CORRECTLY — 10 is the current version.
		// ⚠️ The residual is the DEVELOPER one store.go's bump log records and
		// accepts: a mid-window database held across builds gets a clean Open
		// and "no such column" at the first enrolment. Delete the file — the
		// window does not bump between its sub-chunks.
		//
		// `name` stays TEXT NOT NULL and the parts stay nullable, which is
		// D-48 in the schema: the composed name is the fact this table is
		// required to hold, and the parts are extras that may be absent and
		// may disagree.
		//
		// W5b (item 2) added `phone`, on the same terms and inside this same
		// one CREATE literal (W5-X1). It is NULLABLE and stays nullable, with
		// no default: mandatoriness is an EDGE property of one handler
		// (D-52), never a storage property, and a DEFAULT would turn "no
		// phone" into a fabricated value and hide the gap D-52 accepts. A
		// wire-received person with no phone must fold clean, so a NOT NULL
		// column here would stall the log rather than record the truth.
		//
		// W5c (item 7) added `pad_oriented`, LAST in W5-X3's landing order
		// and inside this same one CREATE literal (W5-X1).
		// ⟦SchemaVersion 11⟧ IT IS STRUCK AGAIN, inside this same literal,
		// at the 10 → 11 increment ([[D-88]] + ERRATA, [[D-91]]): one flag on
		// the enrolling site's row cannot hold a per-site orientation, and
		// D-88 REVERSES the D-10/D-21.1 rulings that put it here ("do not
		// build a keyed person_orientation table" is reversed with them —
		// the keyed table is now the ruled shape, minted by the ORIENTING
		// post in a later stage of this increment). The column is struck,
		// not migrated: the v11 log starts empty, as the v10 log did.
		// ⟦was: the DR-8 paragraph — nullable TEXT, no default, INTEGER as
		// mutant (m5c-5), NOT NULL/DEFAULT as (m5c-4), one column here and
		// one on person_amendment (W5c-3)⟧
		//
		// ⚠ W5b DOES NOT BUMP store.SchemaVersion, and neither does W5a —
		// the paragraph above is STALE and its correction is assigned to
		// W5a's builder, not to this one (reports/W5-POSTLOCK-AMENDMENTS.md
		// §9.7 item 3). The 2026-09-01 adjudication
		// (reports/W5-SCHEMAVERSION-ADJUDICATION.md, landed as amendment §9,
		// which SUPERSEDES §1) ruled within-window bumping wrong: the
		// schema-10 window carries exactly ONE increment, W1's 9 → 10, and
		// every W5 sub-chunk rides it. No stored database inside the window
		// can meet a differently-shaped binary — the v10 log starts empty
		// (D-29), the cutover deletes every database, and one binary ships to
		// both posts. The shape rule quoted above is a rule about crossing a
		// DEPLOYMENT boundary, and this window crosses none.
		// ⚠ THE NEXT WINDOW MUST BUMP.
		// ⚠ W5c DOES NOT BUMP IT EITHER, on exactly the same ruling. Added
		// as its own line rather than by editing W5b's sentence above,
		// because that sentence is another sub-chunk's landed record.
		Schema: `CREATE TABLE IF NOT EXISTS person (
			person_id    TEXT PRIMARY KEY,
			site_id      TEXT NOT NULL,
			name         TEXT NOT NULL,
			company_id   TEXT,
			trade_id     TEXT,
			recorded_by  TEXT,
			last_name    TEXT,
			first_name   TEXT,
			position     TEXT,
			phone        TEXT
		) STRICT;`,
		Apply: applyPersonCreated,
	}
}

func applyPersonCreated(tx *sql.Tx, env store.Envelope) error {
	var p PersonCreated
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return fmt.Errorf("domain: person fold: %w", err)
	}
	if p.Type != "person_created" {
		return nil
	}
	// The fold NEVER refuses (W3-2): a person_created arriving from the other
	// post with no parts at all, with no phone, or with parts that do not
	// compose to its name, lands exactly as sent. Requiredness lives at the
	// intake edge and nowhere else — not in the payload shape, not here, and
	// not in createPerson either, which sits above this fold and below that
	// edge and would break gate add-on-demand (D-52).
	//
	// The values go in as they arrived. Phone in particular is written
	// VERBATIM (W5b-0): normalising here would rewrite what the other post
	// authored, which is the one thing a fold may never do.
	//
	// ⟦SchemaVersion 11⟧ A `pad_oriented` key in the payload — a divergent
	// peer's, since a schema-10 envelope is refused at Apply before any
	// fold sees it — decodes into nothing (the field is struck) and lands
	// nowhere; the fold still never refuses.
	_, err := tx.Exec(`INSERT INTO person (person_id, site_id, name, company_id, trade_id, recorded_by, last_name, first_name, position, phone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.PersonID, env.SiteID, p.Name, p.CompanyID, p.TradeID, p.RecordedBy, p.LastName, p.FirstName, p.Position, p.Phone)
	if err != nil {
		return fmt.Errorf("domain: person fold: insert %s: %w", p.PersonID, err)
	}
	return nil
}

// --- creation-time API (authoring, NOT fold code — folds must never call
// Author, C2 review L2: SetMaxOpenConns(1) means an Author call from inside a
// fold's own transaction deadlocks forever on the connection its own
// transaction holds) ---

// CreateCompany authors a new company with NO recorded_by (kept for the
// callers this signature already has — SeedFixture, and every existing test
// — none of which have a session officer in scope). It is a thin wrapper
// over createCompany; see CreateCompanyRecordedBy for the attributed form
// request-scoped callers (F2) use instead. Open set, add-on-demand: no dedup
// check against existing companies — two sites (or the same site) minting
// "Kakivik" independently is the design's own example of a case that must
// NOT be arbitrated (§8.7 case 2). The UI-level "not on the roster, add it?"
// confirm step itself is C8's (chunk-plan C5 Builds note, M6).
func CreateCompany(st *store.Store, siteID string, nodeEpoch int64, name string, occurredAtMS int64, timeSource string) (Company, error) {
	return createCompany(st, siteID, nodeEpoch, name, nil, occurredAtMS, timeSource)
}

// CreateCompanyRecordedBy is CreateCompany, attributed (F2): recordedBy is
// the session officer in scope at the request that triggered this creation
// (e.g. gate check-in add-on-demand, checkin.go's resolvePersonSlot), or nil
// if none is picked — NEVER a fabricated placeholder (F20's nil contract:
// "a fabricated attribution on a sign-in log is worse than an honest absence
// of one" applies here exactly as it does to visit/trip/arrival).
func CreateCompanyRecordedBy(st *store.Store, siteID string, nodeEpoch int64, name string, recordedBy *string, occurredAtMS int64, timeSource string) (Company, error) {
	return createCompany(st, siteID, nodeEpoch, name, recordedBy, occurredAtMS, timeSource)
}

func createCompany(st *store.Store, siteID string, nodeEpoch int64, name string, recordedBy *string, occurredAtMS int64, timeSource string) (Company, error) {
	if name == "" {
		return Company{}, fmt.Errorf("domain: company name is required")
	}
	c := Company{CompanyID: newID(siteID, "company"), SiteID: siteID, Name: name, RecordedBy: recordedBy}
	payload, err := json.Marshal(CompanyCreated{Type: "company_created", CompanyID: c.CompanyID, Name: name, RecordedBy: recordedBy})
	if err != nil {
		return Company{}, fmt.Errorf("domain: create company: %w", err)
	}
	if _, err := st.Author(siteID, nodeEpoch, payload, occurredAtMS, timeSource); err != nil {
		return Company{}, fmt.Errorf("domain: create company: %w", err)
	}
	return c, nil
}

// TradeCreateRefusal is the trade creation edge's ONE officer-reachable
// refusal — a name already on this site's list — distinguished from an
// internal failure BY TYPE (errors.As), never by reading the error string.
// [[D-76]] (2026-09-05): web/trade.go's friendly-refusal branch tested
// `strings.Contains(err.Error(), "...trade list")` against an error that
// said "...list", so it matched nothing and a duplicate answered a raw 500;
// and the u8-post19 pass measured why text is the wrong key in general — a
// %w→%s swap leaves .Error() byte-identical while breaking the chain. The
// refusal carries the ON-LIST spelling so the page can name the row to pick.
type TradeCreateRefusal struct {
	// Existing is the name AS IT STANDS ON THE LIST — under the D-76 fold it
	// may differ from Typed in case or padding, and it is the row the officer
	// should pick.
	Existing string
	// Typed is what the caller asked to create.
	Typed string
}

func (e *TradeCreateRefusal) Error() string {
	return fmt.Sprintf("domain: the trade %q is already on this site's list (refused: trade names are unique site-wide); pick it from the list instead of adding it again", e.Typed)
}

// sameTradeName is THE ONE RULE for "these two spellings are one trade"
// [[D-76]]: case and leading/trailing whitespace fold, nothing else. Both
// doors key on it — createTrade's refusal and checkin.go's
// resolveOrCreateTradeByName's resolve step — so no door can mint a second
// spelling of a listed trade, and the gate ([[D-66]]: never refuses) RESOLVES
// every spelling the admin screen would refuse.
func sameTradeName(a, b string) bool {
	// ⟦R-087, 2026-10-03⟧ inner whitespace RUNS fold too: "Cook  Helper" and
	// "Cook Helper" were accepted as two trades (L06 R-087, measured twice),
	// and a browser collapses the run before the heading is seen, so the
	// muster printed two headings that read as one. Still case and space,
	// nothing else — punctuation ("Cook." / "Cook") stays distinct, as
	// D-76's "nothing else" has it; widening past whitespace needs a ruling.
	return strings.EqualFold(foldNameSpaces(a), foldNameSpaces(b))
}

// foldNameSpaces trims and collapses every whitespace run to one space —
// what a browser does to a heading's text before anyone reads it.
func foldNameSpaces(s string) string { return strings.Join(strings.Fields(s), " ") }

// SameTradeName is sameTradeName for the web layer's pre-checks (intake's
// add-a-trade door), so the officer-facing "already on the list" sentence
// and the domain's own refusal fire on exactly the same pairs.
func SameTradeName(a, b string) bool { return sameTradeName(a, b) }

// SameCompanyName is checkin.go's sameCompanyName for the same callers —
// intake's add-a-company pre-check and the company rename guard both read
// the ONE company fold rather than respelling it.
func SameCompanyName(a, b string) bool { return sameCompanyName(a, b) }

// IsRetired ⟦[[D-21.5]], R-118, 2026-10-03⟧ answers "has this person been
// retired from the picker?" for every SELECTION surface — the gate's
// type-ahead and typed-name resolution, the arrival picker's roster band,
// the orientation page's tick list. It is read-side and a pure function of
// the composed `retired` fact, so every post holding the same rows answers
// identically. It is deliberately NOT consulted by the muster, the on-site
// lists, check-out or anything that reads a VISIT: retirement affects
// selection, never the record — a retired person who is still on site
// still appears everywhere a visit puts them (D-21.5's own words).
func (p Person) IsRetired() bool { return isRetiredValue(p.Retired) }

// isRetiredValue is the retired fact's value domain, read tolerantly: the
// edge (AmendPerson) only ever authors "true", and a peer's divergent "yes"
// or "1" still reads as retired rather than as nothing. Any other value —
// including a wire-received "false" — reads as not retired; un-retiring is
// the Clear, never a value.
func isRetiredValue(v *string) bool {
	if v == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(*v)) {
	case "true", "yes", "1":
		return true
	}
	return false
}

// CreateTrade authors a new trade with NO recorded_by — see CreateCompany's
// comment above; the same wrapper/attributed split applies.
//
// ⚠ THE UNIQUENESS INVARIANT IS SITE-WIDE ON NAME ⟦DR-W8-11⟧, and the change
// from its predecessor is a deletion rather than a weakening: a trade is not
// scoped to a company at all ⟦DR-W8-2⟧, so there is no company predicate to
// scope the check by. A refusal never calls Author — nothing is written to
// the log.
//
// ⚠ IT REFUSES A COLLIDING NAME AND NOTHING ELSE. There is no retired-trade
// refusal: D-30(b) rules that trade has no retirement, so A §10.5's "the
// authoring edge refuses a RETIRED trade_id" is a DEAD ARM and is named here
// so it is not built.
func CreateTrade(st *store.Store, siteID string, nodeEpoch int64, name string, occurredAtMS int64, timeSource string) (Trade, error) {
	return createTrade(st, siteID, nodeEpoch, name, nil, occurredAtMS, timeSource)
}

// CreateTradeRecordedBy is CreateTrade, attributed (F2) — see
// CreateCompanyRecordedBy's comment for the nil contract.
func CreateTradeRecordedBy(st *store.Store, siteID string, nodeEpoch int64, name string, recordedBy *string, occurredAtMS int64, timeSource string) (Trade, error) {
	return createTrade(st, siteID, nodeEpoch, name, recordedBy, occurredAtMS, timeSource)
}

func createTrade(st *store.Store, siteID string, nodeEpoch int64, name string, recordedBy *string, occurredAtMS int64, timeSource string) (Trade, error) {
	if name == "" {
		return Trade{}, fmt.Errorf("domain: a trade name is required")
	}
	// [[G12]] (2026-09-08, James's ruling): compared against the COMPOSED
	// list, not the base rows — same basis as Trades()'s other two readers,
	// AmendTrade's Arm 10 rename guard and the gate's add-on-demand resolve
	// step (resolveOrCreateTradeByName). A rename is an amendment layered on
	// top of the base row; dumpSection never sees it, so after a rename the
	// base scan compared against a name nobody holds and missed the one
	// somebody does — refusing the gate's add-it escape on the pre-rename
	// name (a live D-66/D-71 violation) and letting a second identical
	// heading be minted on the post-rename name. See
	// reports/witness/G12-CREATE-DOOR-BASIS.md.
	existing, err := Trades(st)
	if err != nil {
		return Trade{}, fmt.Errorf("domain: create trade: %w", err)
	}
	// [[D-76]]: the comparison FOLDS case and leading/trailing whitespace —
	// "Welder" and "welder" are one heading on the muster sheet, and this
	// door used to mint the second spelling. The refusal is TYPED so the web
	// layer matches it with errors.As, never by reading this text.
	for _, w := range existing {
		if sameTradeName(w.Name, name) {
			return Trade{}, &TradeCreateRefusal{Existing: w.Name, Typed: name}
		}
	}
	tr := Trade{TradeID: newID(siteID, "trade"), SiteID: siteID, Name: name, RecordedBy: recordedBy}
	payload, err := json.Marshal(TradeCreated{
		Type: evtTradeCreated, TradeID: tr.TradeID, Name: name, RecordedBy: recordedBy,
	})
	if err != nil {
		return Trade{}, fmt.Errorf("domain: create trade: %w", err)
	}
	if _, err := st.Author(siteID, nodeEpoch, payload, occurredAtMS, timeSource); err != nil {
		return Trade{}, fmt.Errorf("domain: create trade: %w", err)
	}
	return tr, nil
}

// CreatePerson authors a new person with NO recorded_by — see CreateCompany's
// comment above; the same wrapper/attributed split applies. Open set,
// add-on-demand, no dedup check (people are not deduplicated by name).
// companyID/tradeID are the roster DEFAULT only (design §8.8-i) — a visit
// snapshots company/trade at check-in as its own denormalised copy, so
// changing a person's default here later never rewrites a historical visit
// row.
func CreatePerson(st *store.Store, siteID string, nodeEpoch int64, name string, companyID, tradeID *string, occurredAtMS int64, timeSource string) (Person, error) {
	return createPerson(st, siteID, nodeEpoch, name, companyID, tradeID, nil, PersonDetails{}, occurredAtMS, timeSource)
}

// CreatePersonRecordedBy is CreatePerson, attributed (F2) — see
// CreateCompanyRecordedBy's comment for the nil contract. This is the
// creation site that matters most: add-on-demand at the gate mid-check-in
// (checkin.go's resolvePersonSlot) is how most persons enter the roster.
//
// It carries NO PersonDetails, and that is D-52's ruling in the signature:
// a gate officer with a person standing in front of them types one name box
// and is not asked for parts — and, as of W5b, is not asked for a phone
// number either. James ruled this door EXEMPT from the phone requirement in
// terms. The edge that DOES collect those facts uses
// CreatePersonWithDetails below.
func CreatePersonRecordedBy(st *store.Store, siteID string, nodeEpoch int64, name string, companyID, tradeID, recordedBy *string, occurredAtMS int64, timeSource string) (Person, error) {
	return createPerson(st, siteID, nodeEpoch, name, companyID, tradeID, recordedBy, PersonDetails{}, occurredAtMS, timeSource)
}

// PersonDetails carries the OPTIONAL created-state person facts (W5a, item 1
// + D-21.4). Every field is a pointer and nil means ABSENT — never asserted —
// which is a different thing from empty-string (W3-3's distinction, kept
// here because the fold and the projection both preserve it).
//
// It exists so that an edge which HAS collected these facts can record them
// without every other authoring path being widened. Two calling rules, both
// load-bearing:
//
//   - Nothing in here is ever derived from `name`. Composition is one-way —
//     parts to Name via composeName at the authoring edge, NEVER Name to
//     parts anywhere (W5a-8). A caller that has only a composed name passes
//     PersonDetails{}.
//   - Requiredness is NOT this type's business, nor createPerson's. A
//     "both parts or refuse" check placed below an edge sits above the fold
//     and below the edge, breaks gate add-on-demand, and passes every
//     intake-only test (D-52, mutant (m5a-7)).
type PersonDetails struct {
	LastName  *string
	FirstName *string
	// Position is free text with no taxonomy: no enum, no validation against
	// a list, no branch on its content (D-21.4).
	Position *string
	// Phone (W5b, item 2) is free text too, and is recorded VERBATIM — no
	// digit-stripping, no space/dash/paren removal, no country code, no
	// truncation, no case-fold, here or anywhere below here.
	//
	// D-52 is the reason it is HERE and not on CreatePersonRecordedBy's
	// signature: the gate's door must keep asking for nothing. An edge that
	// has collected a phone number passes one; the gate passes
	// PersonDetails{} and authors a person with none, which is the ruling.
	Phone *string
}

// CreatePersonWithDetails is CreatePersonRecordedBy plus the optional
// created-state facts of PersonDetails — the door for an edge that collects
// them, which today is roster intake (web/intake.go, which already asks for
// Last and First as two boxes and, before W5a, threw both away after
// composing Name from them; W5b added the phone box beside them).
//
// A separate entry point rather than a widened CreatePersonRecordedBy is a
// deliberate choice with two reasons. The gate's door (D-52) must keep
// asking for nothing, and a signature that cannot carry parts cannot
// accidentally acquire them; and the existing wrappers are called from ~50
// test sites this chunk may not edit, so widening them would have meant
// editing tests to land production code.
func CreatePersonWithDetails(st *store.Store, siteID string, nodeEpoch int64, name string, companyID, tradeID, recordedBy *string, details PersonDetails, occurredAtMS int64, timeSource string) (Person, error) {
	return createPerson(st, siteID, nodeEpoch, name, companyID, tradeID, recordedBy, details, occurredAtMS, timeSource)
}

func createPerson(st *store.Store, siteID string, nodeEpoch int64, name string, companyID, tradeID, recordedBy *string, details PersonDetails, occurredAtMS int64, timeSource string) (Person, error) {
	// The ONE requirement at this level, and it is the display name's:
	// `Name` is required and authoritative (plan `:2029`; D-48 rules the
	// authority half). The parts are extras — a person with neither is
	// ordinary here, because the gate authors exactly that.
	//
	// ⚠ PHONE IS NOT CHECKED HERE, and that is D-52 in the code rather than
	// in a comment. This helper is reached from BOTH creation doors: intake,
	// where a phone is mandatory, and add-on-demand at the gate, where it is
	// not. A phone check placed here sits above the fold and below the edge —
	// it satisfies every intake-only test and breaks how most persons enter
	// the roster. The requirement lives in web/intake.go's handleIntake and
	// nowhere else. (m5b-1)'s second arm is exactly this mutant.
	if name == "" {
		return Person{}, fmt.Errorf("domain: person name is required")
	}
	p := Person{
		PersonID: newID(siteID, "person"), SiteID: siteID, Name: name,
		CompanyID: companyID, TradeID: tradeID, RecordedBy: recordedBy,
		LastName: details.LastName, FirstName: details.FirstName, Position: details.Position,
		Phone: details.Phone,
	}
	payload, err := json.Marshal(PersonCreated{
		Type: "person_created", PersonID: p.PersonID, Name: name, CompanyID: companyID, TradeID: tradeID, RecordedBy: recordedBy,
		LastName: details.LastName, FirstName: details.FirstName, Position: details.Position,
		Phone: details.Phone,
	})
	if err != nil {
		return Person{}, fmt.Errorf("domain: create person: %w", err)
	}
	if _, err := st.Author(siteID, nodeEpoch, payload, occurredAtMS, timeSource); err != nil {
		return Person{}, fmt.Errorf("domain: create person: %w", err)
	}
	return p, nil
}

// --- reads (for C8 roster autocomplete, C9 muster, and this chunk's own
// creation-time checks) ---

// Companies returns the COMPOSED companies (W5d-3): the created-state rows
// with every company_amended assertion folded over them in ascending key
// order. This is THE one composed company read path — every reader (the
// roster page, the gate picker, the live muster's grouping, the trip
// author) resolves through it or through CompanyByID, never through the
// raw projection row (W5d-5).
func Companies(st *store.Store) ([]Company, error) {
	base, err := dumpSection[Company](st, "company")
	if err != nil {
		return nil, err
	}
	amendments, err := readCompanyAmendments(st)
	if err != nil {
		return nil, err
	}
	return composeCompanies(base, amendments), nil
}

// Trades returns the COMPOSED trades (W8-3): the created-state rows with
// every `trade_amended` assertion folded over them in ascending key order.
// This is THE one composed trade read path — every reader resolves through
// it or through TradeByID, never through the raw projection row.
func Trades(st *store.Store) ([]Trade, error) {
	base, err := dumpSection[Trade](st, "trade")
	if err != nil {
		return nil, err
	}
	amendments, err := readTradeAmendments(st)
	if err != nil {
		return nil, err
	}
	return composeTrades(base, amendments), nil
}

// Persons returns the COMPOSED people (W3-8): the created-state rows with
// every person_amended assertion folded over them in ascending key order.
// This is THE one composed read path — every reader (picker, roster, C-9
// muster display, dump person data) resolves through it, never through the
// raw projection row (W3-4).
func Persons(st *store.Store) ([]Person, error) {
	base, err := dumpSection[Person](st, "person")
	if err != nil {
		return nil, err
	}
	amendments, err := readPersonAmendments(st)
	if err != nil {
		return nil, err
	}
	return composePersons(base, amendments), nil
}

// Point lookups (F30): roster checks on the authoring hot paths (a check-in
// validates single digits of person ids) resolve by primary key instead of
// decoding the whole roster per call. nil = no such row here.

// PersonByID returns the COMPOSED person (see Persons above), or nil when
// no such row exists here.
func PersonByID(st *store.Store, id string) (*Person, error) {
	p, err := oneRow[Person](st, "person", store.Eq{Column: "person_id", Value: id})
	if err != nil || p == nil {
		return p, err
	}
	// M-1 (W3 build critique): scoped read — only this person's amendment
	// rows, filtered in SQL, same ascending-key order. Composing them here
	// (composePerson — the same per-fact resolution Persons() applies) is
	// exactly the old read-everything-then-filter-in-Go loop, minus the
	// whole-table decode per call.
	amendments, err := readPersonAmendmentsFor(st, id)
	if err != nil {
		return nil, err
	}
	composePerson(p, amendments)
	return p, nil
}

// CompanyByID returns the COMPOSED company (see Companies above), or nil
// when no such row exists here.
func CompanyByID(st *store.Store, id string) (*Company, error) {
	c, err := oneRow[Company](st, "company", store.Eq{Column: "company_id", Value: id})
	if err != nil || c == nil {
		return c, err
	}
	// The M-1 shape is not cloned (W5d-4): a SCOPED read — only this
	// company's amendment rows, filtered in SQL, in the same ascending-key
	// order, sharing the select half and the one canonical ORDER BY
	// spelling with the whole-table read.
	amendments, err := readCompanyAmendmentsFor(st, id)
	if err != nil {
		return nil, err
	}
	// ⟦SchemaVersion 11, [[D-92]]⟧ composed through the ONE per-fact rule
	// (composeCompany → resolveContest); folding the scoped rows one at a
	// time is identical to filtering the whole read, as before. ⟦was: "for
	// _, r := range amendments { applyCompanyAmendmentRow(c, r) }"⟧
	composeCompany(c, amendments)
	return c, nil
}

// TradeByID returns the COMPOSED trade (see Trades above), or nil when no
// such row exists here.
//
// ⚠ THE AMENDMENT READ IS SCOPED IN SQL (M-1, the shape W5d set and W7
// cloned): only this trade's amendment rows, filtered by `trade_id`, in the
// same ascending-key order, sharing the select half and the ONE canonical
// ORDER BY spelling with the whole-table read. A whole-table decode per
// lookup is what M-1 exists to prevent, and the gate's check-in path and the
// muster's grouping both resolve trades per row.
func TradeByID(st *store.Store, id string) (*Trade, error) {
	tr, err := oneRow[Trade](st, "trade", store.Eq{Column: "trade_id", Value: id})
	if err != nil || tr == nil {
		return tr, err
	}
	amendments, err := readTradeAmendmentsFor(st, id)
	if err != nil {
		return nil, err
	}
	// ⟦SchemaVersion 11, [[D-92]]⟧ composed through the ONE per-fact rule
	// (composeTrade → resolveContest). ⟦was: "for _, r := range amendments
	// { applyTradeAmendmentRow(tr, r) }"⟧
	composeTrade(tr, amendments)
	return tr, nil
}
