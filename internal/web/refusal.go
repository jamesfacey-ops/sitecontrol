// refusal.go — the wording layer every refusal and notice goes through
// (W4, 2026-10-03). Two halves, one rule: an officer never reads an
// internal error string.
//
//  1. officerText is THE mapper. Domain and store errors are written as
//     "<package>: <operation>: <sentence>" — the prefix chain is for the
//     log, the sentence is for the officer. It strips the chain, drops the
//     design-document citations ("(§9.2)", "(D-4)") an officer has never
//     seen, and replaces a message that is a fault rather than a refusal
//     (an SQL error, a decode failure, an id lookup that came back short)
//     with one plain sentence, logging the raw text so nothing is lost.
//     It NEVER changes whether something is refused — only what the
//     refusal says.
//
//  2. refusalPages is the shared render for the refusals that used to go
//     out as http.Error: a bare text/plain body on a blank screen, no top
//     bar, no way back. Every such response (status >= 400, text/plain) on
//     an officer route is replaced, at the same honest status code, by a
//     full page carrying the top bar and ONE notice whose bold lead says
//     what happened ("NOT RECORDED." on a refused form). Machine routes
//     (/sync/*, /debug/*) are passed through untouched.
//
// Pages that re-render their own form on a refusal (the gate, the amend
// screens, time correction) call officerText where they fill their Error
// field, so the typed form survives AND the sentence is plain.
//
// Lead's brief (2026-10-03): "ONE shared mapper ... used by every
// refusal/notice path". TestNoRenderedRefusalCarriesAnInternalPrefix
// (refusal_test.go) is the sweep that holds it.
package web

import (
	"bytes"
	"log"
	"net/http"
	"regexp"
	"strings"

	"sitecontrol/internal/config"
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/store"
)

// faultSentence is what an officer reads in place of a message that is a
// fault in this post rather than a refusal of what they did. It does not
// claim anything was or was not recorded — the surrounding page says that
// where it is known (a refused form's lead, the gate's "Nothing was
// recorded.").
const faultSentence = "This post could not complete that. Try again, and if it repeats, radio it through."

// diskFullSentence ⟦FIX17b, sc-stress F10⟧ is what an officer reads when a
// write is refused because this post's disk is full. ⟦was: the fault
// sentence or "database error" — nothing said the disk was full or what to
// do⟧ It is not a retry: nothing will record until space is freed.
const diskFullSentence = "THIS POST'S DISK IS FULL. Nothing was recorded, and nothing more can be recorded here until space is freed. Call IT now. Pages that only show records still work."

// chainLinkRe finds every link of an error's context chain: a package
// name ("domain", "store", "repl") at the start, or after the ": " that
// %w-wrapping puts between an outer message and the error it wraps.
var chainLinkRe = regexp.MustCompile(`(?:^|: )(?:domain|store|repl|deadline|config|web): `)

// opLabelRe is the operation label that follows a package name
// ("dispatch: ", "amend person: ", "VisitsByID: "): up to 40 characters
// with no colon, so it can never reach into the sentence after it.
var opLabelRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_ ().,-]{0,40}: `)

// citationRe matches a parenthesised design-document SECTION citation and
// nothing else: "(§9.2)", "(TP §4.7)". A parenthesis that says anything
// beyond the citation is kept — it is a sentence, not a reference. A
// decision number ("(D-87)") is not matched here, but none should reach an
// officer: FIX5 (2026-10-05) took "(D-87)" out of store.OwnershipRule and
// the two arrival refusal templates, and the D-87 witness tests now pin
// its absence. ⟦was: "James's decision numbers ("(D-87)") are kept: they
// name the rule an officer is being held to"⟧
var citationRe = regexp.MustCompile(`\s*\((?:[A-Z]{1,3} )?§[\d.]+\)`)

// trailingCitationRe is the other spelling: a citation tucked at the end of
// a parenthetical that DOES say something — "(and is always an occupant,
// §7)" keeps its words and loses only ", §7".
var trailingCitationRe = regexp.MustCompile(`,\s*(?:[A-Z]{1,3} )?§[\d.]+\)`)

// leadingRefusedRe drops the word "refused" a domain sentence opens with:
// every page that shows the sentence already says it was refused, in bold,
// first.
var leadingRefusedRe = regexp.MustCompile(`^(?i:refused)\s*(?:—|-|:|;)\s*`)

// visitsNotFoundRe is the muster reprint's loud failure
// (domain.VisitsByID). The ids ARE the information — a reprint that cannot
// find someone must say who — so this shape is rewritten, not genericised.
var visitsNotFoundRe = regexp.MustCompile(`VisitsByID: (\d+) visit id\(s\) not found: (.*)$`)

// faultMarkers are substrings that only ever appear in a fault: a driver
// error, an encoder error, an internal invariant. None of them occurs in
// any refusal sentence the domain writes for an officer (checked against
// every "domain: …" format string in internal/domain, 2026-10-03).
var faultMarkers = []string{
	"sql:", "sqlite", "SQL logic", "constraint failed", "database is locked",
	"no such table", "no such column", "database error",
	"json:", "unmarshal", "decode", "strconv.", "invalid syntax",
	"unexpected EOF", "context deadline", "context canceled",
	"nil pointer", "runtime error", "%!",
	"internal fault", "registered carrier", "resolveContest", " fold",
	"projection", "entry snapshot", "carries no readable",
}

// officerText maps one internal error string to the sentence an officer
// reads. An empty string stays empty (no refusal, nothing to say). The
// sentence's own first letter is left alone — it may be an id ("visit-…"),
// and every surface shows it after a bold lead and a dash.
func officerText(raw string) string {
	msg := strings.TrimSpace(raw)
	if msg == "" {
		return ""
	}
	if store.TextIsDiskFull(msg) {
		log.Printf("refusal: disk full: %s", raw)
		return diskFullSentence
	}
	if m := visitsNotFoundRe.FindStringSubmatch(msg); m != nil {
		return "This print link names " + m[1] + " visit record(s) this post cannot find (" + strings.TrimSpace(m[2]) +
			"). The sheet was not printed, so nobody is silently missing from it — open the muster and print it again from there."
	}
	switch msg {
	case "bad form":
		return "That form did not arrive intact. Reload the page and enter it again."
	case "database error":
		log.Printf("refusal: fault shown to officer as plain copy: %s", raw)
		return "This post could not read its records just now. Try again, and if it repeats, radio it through."
	}
	msg = unchain(msg, raw)
	if strings.Contains(msg, "matches no local event") {
		return "There is no record at this post with that key yet. Check the key; if it was made at another post it may not have reached this one yet."
	}
	msg = leadingRefusedRe.ReplaceAllString(msg, "")
	msg = trailingCitationRe.ReplaceAllString(msg, ")")
	msg = citationRe.ReplaceAllString(msg, "")
	msg = strings.TrimSpace(msg)
	if msg == "" {
		log.Printf("refusal: nothing left to show after mapping: %s", raw)
		return faultSentence
	}
	return msg
}

// unchain splits an error chain into its links and keeps what an officer
// can use: the INNERMOST link (the actual reason) and any outer link that
// is a sentence rather than a label — "the check-outs before it ARE
// recorded, the rest are not" is context the officer must read, "insert
// x" or "add-on-demand company" is not. A link that is a fault becomes
// faultSentence (logged raw); a fault in an outer link is dropped.
func unchain(msg, raw string) string {
	locs := chainLinkRe.FindAllStringIndex(msg, -1)
	if len(locs) == 0 {
		if isFault(msg) {
			log.Printf("refusal: fault shown to officer as plain copy: %s", raw)
			return faultSentence
		}
		return msg
	}
	var links []string
	if locs[0][0] > 0 {
		links = append(links, msg[:locs[0][0]])
	}
	for i, loc := range locs {
		end := len(msg)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		link := msg[loc[1]:end]
		if i+1 < len(locs) || opLabelRe.MatchString(link) {
			link = opLabelRe.ReplaceAllString(link, "")
		}
		links = append(links, link)
	}
	cause := strings.TrimSpace(links[len(links)-1])
	if isFault(cause) {
		log.Printf("refusal: fault shown to officer as plain copy: %s", raw)
		cause = faultSentence
	}
	var kept []string
	for _, ctx := range links[:len(links)-1] {
		ctx = strings.TrimSpace(ctx)
		if ctx == "" || isFault(ctx) || labelLike(ctx) {
			continue
		}
		kept = append(kept, ctx)
	}
	return strings.Join(append(kept, cause), ": ")
}

// labelLike reports an outer chain link that names an operation rather
// than telling the officer anything: short, and no sentence punctuation.
func labelLike(s string) bool {
	if strings.ContainsAny(s, ";.") || strings.Contains(s, " — ") {
		return false
	}
	return len(strings.Fields(s)) <= 6
}

func isFault(s string) bool {
	for _, marker := range faultMarkers {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

// refusalText is officerText for an error value.
func refusalText(err error) string {
	if err == nil {
		return ""
	}
	return officerText(err.Error())
}

// --- the shared refusal page ---

// refusalHasFormState says whether "the Back button returns to the form with
// what you typed still in it" is true. James, 2026-10-04 (T-097): it is not
// for the one-button acknowledgment posts (nothing was typed), nor for a
// POST that arrived with no page behind it (a crafted or scripted request:
// there is no form to go back to, at this post or any other).
func refusalHasFormState(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	switch r.URL.Path {
	case "/alerts/ack", "/alerts/unack":
		return false
	}
	return refererPath(r) != ""
}

// refusalPageData is the page that replaces a bare-text refusal.
type refusalPageData struct {
	Cfg     *config.Config
	SiteID  string
	Lead    string // the bold KIND, first thing read, then " — " and the sentence
	Message string // officerText of what the handler said
	Back    string // same-origin path of the page the officer came from
	// FromPOST picks the way-back sentence: after a refused form the
	// browser's Back button still holds the typing; a plain link does not.
	FromPOST bool
	// Status is the refused response's code (a 405 has no form behind it).
	Status int
	// The alert strip rides this page like every other (§9.4): an officer
	// reading a refusal must still see an overdue vehicle.
	Banner    []deadline.Alert
	BannerErr string
}

// refusalLead says what kind of answer this is. A refused FORM (4xx on a
// POST) authored nothing — every POST handler refuses before it appends
// (TestARefusedPostAuthorsNothing) — so it says so. A fault (5xx) says
// only that it could not complete: whether anything landed is not known
// here. A refused READ records nothing either way, so it claims nothing
// about recording.
func refusalLead(method string, status int) string {
	switch {
	case status >= 500:
		return "COULD NOT COMPLETE"
	case method == http.MethodPost:
		return "NOT RECORDED"
	default:
		return "CANNOT SHOW THAT"
	}
}

// refusalCapture holds back a text/plain error response so it can be
// re-rendered as a page. Anything else passes straight through.
type refusalCapture struct {
	http.ResponseWriter
	status      int
	captured    bool
	wroteHeader bool
	body        bytes.Buffer
}

func (c *refusalCapture) WriteHeader(code int) {
	if c.wroteHeader {
		return
	}
	c.wroteHeader = true
	c.status = code
	if code >= 400 && strings.HasPrefix(c.Header().Get("Content-Type"), "text/plain") {
		c.captured = true
		return
	}
	c.ResponseWriter.WriteHeader(code)
}

func (c *refusalCapture) Write(p []byte) (int, error) {
	if !c.wroteHeader {
		c.WriteHeader(http.StatusOK)
	}
	if c.captured {
		return c.body.Write(p)
	}
	return c.ResponseWriter.Write(p)
}

// machineRoute reports a path whose callers are programs, not officers:
// their error bodies are a protocol and stay exactly as written.
func machineRoute(path string) bool {
	return strings.HasPrefix(path, "/sync/") || strings.HasPrefix(path, "/debug/")
}

// refusalPages wraps the mux (see Root): every bare-text error response on
// an officer route becomes the shared refusal page, same status code.
func (s *Server) refusalPages(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if machineRoute(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		c := &refusalCapture{ResponseWriter: w}
		next.ServeHTTP(c, r)
		if !c.captured {
			return
		}
		raw := c.body.String()
		h := w.Header()
		h.Del("Content-Length")
		h.Del("X-Content-Type-Options")
		back := refererPath(r)
		if back == "" || back == r.URL.RequestURI() && r.Method != http.MethodPost {
			back = "/gate"
		}
		data := refusalPageData{
			Cfg:      s.Cfg,
			SiteID:   s.Cfg.SiteID,
			Lead:     refusalLead(r.Method, c.status),
			Message:  officerText(raw),
			Back:     safeNext(back),
			FromPOST: refusalHasFormState(r),
			Status:   c.status,
		}
		data.Banner, data.BannerErr = s.banner()
		var buf bytes.Buffer
		if err := s.templates().ExecuteTemplate(&buf, "refusal.html", data); err != nil {
			// The page itself could not be built: still never the raw text.
			log.Printf("refusal render: %v (original: %s)", err, raw)
			h.Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(data.Lead + " — " + data.Message + "\n"))
			return
		}
		h.Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(c.status)
		_, _ = w.Write(buf.Bytes())
	})
}

// Root is the whole served surface, as cmd/sitectl wires it: the mux, the
// shared refusal page around it, the read-only dispatch host's guard
// around that (FIX7, dispatch_surface.go — a write on the dispatch host is
// refused before anything else runs), the top bar's LOST SIGNAL line
// outside that (FIX15, lostsignal.go — outside dispatchIdentity so its
// "this post" rewrite never touches the line), and the theme switch outermost (so
// every page is themed like every other), all inside one read snapshot per GET
// (FIX17c, readview.go). Handler() stays a bare
// *http.ServeMux for the tests that interrogate its routing table.
func (s *Server) Root() http.Handler {
	return s.countRequests(s.readViews(ThemeMiddleware(homeLink(s.lostSignalStrip(s.dispatchIdentity(s.dispatchGuard(s.refusalPages(s.Handler()))))))))
}

// countRequests keeps s.inflight: how many requests this post is serving
// right now. A roster import reads it between batches (yieldToRequests) so
// that it never lands a batch under a page part-way through its reads.
func (s *Server) countRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.inflight.Add(1)
		defer s.inflight.Add(-1)
		next.ServeHTTP(w, r)
	})
}

// dbFailBody is the body a page's read failure is served with: the plain
// disk-full sentence when that is what failed, else "database error" (which
// officerText maps to its own sentence, as it always has).
func dbFailBody(err error) string {
	if store.IsDiskFull(err) {
		return diskFullSentence
	}
	return "database error"
}
