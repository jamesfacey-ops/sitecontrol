package siteadmin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/repl"
	"sitecontrol/internal/store"
)

// HTTPClient is what every network question here goes through; tests swap
// nothing — they serve real repl handlers on httptest listeners.
var HTTPClient = &http.Client{Timeout: 10 * time.Second}

// PeerEpochs is one peer's answer to "what do you hold of MY stream?" —
// the §8.4/§8.6 self-history exchange the rejoin itself uses.
type PeerEpochs struct {
	SiteID string
	Epochs []repl.StreamHead
	Err    error
}

// SurveyOwnEpochs asks every configured peer which of cfg.SiteID's epochs
// it holds, presenting this post's own identity (X-SiteControl-From) and
// the peer's key. A peer answering as someone else is an error.
func SurveyOwnEpochs(ctx context.Context, cfg *config.Config) []PeerEpochs {
	out := make([]PeerEpochs, 0, len(cfg.Peers))
	for _, p := range cfg.Peers {
		pe := PeerEpochs{SiteID: p.SiteID}
		var sr repl.StreamsResponse
		q := url.Values{"site": {cfg.SiteID}, "t1": {"0"}}
		pe.Err = getJSON(ctx, strings.TrimSuffix(p.BaseURL, "/")+"/sync/streams?"+q.Encode(), map[string]string{
			"Authorization": "Bearer " + p.Key, repl.HeaderFrom: cfg.SiteID,
		}, &sr)
		if pe.Err == nil && sr.SiteID != p.SiteID {
			pe.Err = fmt.Errorf("answered as %q, not %q — wrong address in base_url?", sr.SiteID, p.SiteID)
		}
		pe.Epochs = sr.Streams
		out = append(out, pe)
	}
	return out
}

func getJSON(ctx context.Context, u string, hdr map[string]string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	for k, val := range hdr {
		req.Header.Set(k, val)
	}
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("no answer (%s)", plainNetErr(err))
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return fmt.Errorf("refused this post's key (401) — the key in this file for it is not the one it accepts")
	case http.StatusForbidden:
		return fmt.Errorf("refused this post's identity (403)")
	default:
		return fmt.Errorf("answered HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(v)
}

// plainNetErr drops the URL (which carries nothing secret, but is noise).
func plainNetErr(err error) string {
	var ue *url.Error
	if e, ok := err.(*url.Error); ok {
		ue = e
		err = ue.Err
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return "timed out"
	}
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 {
		s = s[i+2:]
	}
	return s
}

// LocalBase is http://<listen_addr> made dialable: a wildcard host becomes
// loopback.
func LocalBase(cfg *config.Config) string {
	host, port, err := net.SplitHostPort(cfg.ListenAddr)
	if err != nil {
		return "http://" + cfg.ListenAddr
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// Answers reports whether ANY HTTP server answers at base (any status: a
// 401 is still a running post). Used to refuse while a post is running.
func Answers(ctx context.Context, base string) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(base, "/")+"/sync/streams", nil)
	if err != nil {
		return false
	}
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

// ResolveDBPath is db_path as the service sees it: relative paths are
// relative to the service's working directory, which every deployment sets
// to the folder holding site.toml (CT510's WorkingDirectory=/opt/sitectl/%i;
// the Windows install puts both in C:\ProgramData\SiteControl\).
func ResolveDBPath(cfgPath string, cfg *config.Config, override string) string {
	if override != "" {
		return override
	}
	if filepath.IsAbs(cfg.DBPath) {
		return cfg.DBPath
	}
	return filepath.Join(filepath.Dir(cfgPath), cfg.DBPath)
}

// LocalEpochs lists the epochs this post's own DATABASE (if any) holds for
// its own site — a disk moved from the old laptop, or a restored backup.
// A missing file is "none", never created.
func LocalEpochs(dbPath, siteID string) ([]int64, error) {
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, nil
	}
	st, err := OpenReadStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	return st.Epochs(siteID)
}

// Restorer is FIX16 D's `-restore`, behind an interface so the swap does
// not depend on that branch landing first (lead, 2026-10-06). The real one
// verifies the copy, refuses while the post runs, and requires node_epoch
// above every epoch the post authored in — the same rule SwapPlan applies.
type Restorer interface {
	Restore(cfg *config.Config, backupPath string, out io.Writer, now time.Time) error
}

// RestorerFunc adapts a function (cmd/sitectl's restoreBackup) to Restorer.
type RestorerFunc func(cfg *config.Config, backupPath string, out io.Writer, now time.Time) error

func (f RestorerFunc) Restore(cfg *config.Config, backupPath string, out io.Writer, now time.Time) error {
	return f(cfg, backupPath, out, now)
}

// UnavailableRestorer is what a caller that wires no restore gets.
type UnavailableRestorer struct{}

func (UnavailableRestorer) Restore(*config.Config, string, io.Writer, time.Time) error {
	return fmt.Errorf("restore from backup is not in this build (FIX16 D's -restore is not merged yet) — copy the old laptop's database into place by hand, or rebuild blank")
}

// SwapOptions are `swap plan`'s flags.
type SwapOptions struct {
	CfgPath       string
	DBOverride    string
	OldURL        string // the old laptop's address, if it differs from listen_addr
	FromBackup    string // restore this backup after raising the epoch
	ForceEpoch    int64  // -epoch N (only with UnreachableOK)
	UnreachableOK bool   // proceed with peers unreachable (N must still clear every answer)
	Restorer      Restorer
	Now           time.Time
}

// SwapPlan prepares a replacement laptop's site.toml: it refuses while the
// old author (or this post's own service) still answers, asks every peer
// for the highest epoch of this post it holds, and writes node_epoch =
// max+1 (with a backup of the old file). Returns the lines to print.
func SwapPlan(ctx context.Context, o SwapOptions) ([]string, error) {
	raw, err := os.ReadFile(o.CfgPath)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Parse(raw, o.CfgPath)
	if err != nil {
		return nil, err
	}
	var lines []string
	say := func(f string, a ...any) { lines = append(lines, fmt.Sprintf(f, a...)) }

	// 1. Nobody may be authoring as this post right now.
	if Answers(ctx, LocalBase(cfg)) {
		return nil, fmt.Errorf("REFUSED: something already answers at %s (this post's own address) — stop the SiteControl service on this laptop, and make sure the OLD laptop is switched off or unplugged: two laptops writing as %s at once would split its records for good", LocalBase(cfg), cfg.SiteID)
	}
	if o.OldURL != "" && Answers(ctx, o.OldURL) {
		return nil, fmt.Errorf("REFUSED: the old laptop still answers at %s — switch it off or unplug it first", o.OldURL)
	}

	// 2. Q2 (James, 2026-10-06): a blank rebuild gets retired posts'
	// records back from the live posts; say so.
	dbPath := ResolveDBPath(o.CfgPath, cfg, o.DBOverride)
	_, statErr := os.Stat(dbPath)
	if n := BlankRebuildNote(cfg, os.IsNotExist(statErr) && o.FromBackup == ""); n != "" {
		say("%s", n)
	}

	// 3. The highest epoch anybody holds for this post.
	var max int64
	unreachable := []string{}
	for _, pe := range SurveyOwnEpochs(ctx, cfg) {
		name := cfg.SiteName(pe.SiteID)
		if pe.Err != nil {
			unreachable = append(unreachable, name)
			say("  %-24s %v", name, pe.Err)
			continue
		}
		var hi, n int64
		for _, s := range pe.Epochs {
			if s.NodeEpoch > hi {
				hi = s.NodeEpoch
			}
			if s.Head > 0 {
				n += s.Head
			}
		}
		say("  %-24s holds %d record(s) of %s, highest epoch %d", name, n, cfg.SiteID, hi)
		if hi > max {
			max = hi
		}
	}
	local, err := LocalEpochs(dbPath, cfg.SiteID)
	if err != nil {
		return nil, fmt.Errorf("reading this laptop's database %s: %w", dbPath, err)
	}
	for _, e := range local {
		if e > max {
			max = e
		}
	}
	if len(local) > 0 {
		say("  %-24s this laptop's database holds epochs %v", "(local)", local)
	}
	if o.FromBackup != "" {
		be, err := store.ReadEpochs(o.FromBackup, cfg.SiteID)
		if err != nil {
			return nil, fmt.Errorf("reading the backup %s: %w", o.FromBackup, err)
		}
		for _, e := range be {
			if e > max {
				max = e
			}
		}
		say("  %-24s the backup holds epochs %v", "(backup)", be)
	}
	need := max + 1
	if len(unreachable) > 0 {
		if !o.UnreachableOK {
			return nil, fmt.Errorf("REFUSED: %s did not answer, so the highest epoch %s has used cannot be proven — fix the link and run again, or (only if that post is truly gone) pass -epoch N -unreachable-ok with N above every epoch it could hold:\n%s",
				strings.Join(unreachable, ", "), cfg.SiteID, strings.Join(lines, "\n"))
		}
		if o.ForceEpoch < need {
			return nil, fmt.Errorf("REFUSED: -epoch %d is not above every answer received (need at least %d)", o.ForceEpoch, need)
		}
		need = o.ForceEpoch
		say("  ⚠ proceeding without %s at the operator's -epoch %d", strings.Join(unreachable, ", "), need)
	}

	// 4. Write it. The epoch in the file itself counts as used: it came
	// from the old laptop (or from IT's copy of it), and the old laptop may
	// have authored records under it that never reached any peer — reusing
	// it would put two different histories under one (site, epoch). So the
	// new epoch is above the file's own too, and every run of `swap plan`
	// raises it (an epoch nobody writes under costs nothing).
	if cfg.NodeEpoch > max {
		max = cfg.NodeEpoch
	}
	if o.UnreachableOK && o.ForceEpoch > 0 && o.ForceEpoch <= max {
		return nil, fmt.Errorf("REFUSED: -epoch %d is not above %d (the file's own epoch or an answer received)", o.ForceEpoch, max)
	}
	if need <= max {
		need = max + 1
	}
	text, err := setTopLevel(string(raw), "node_epoch", strconv.FormatInt(need, 10))
	if err != nil {
		return nil, err
	}
	nc, err := config.Parse([]byte(text), o.CfgPath+" (edited)")
	if err != nil {
		return nil, fmt.Errorf("REFUSED: the edited file would not start: %w", err)
	}
	bak, err := BackupThenWrite(o.CfgPath, []byte(text), o.Now)
	if err != nil {
		return nil, err
	}
	say("node_epoch %d -> %d (old file kept as %s)", cfg.NodeEpoch, need, filepath.Base(bak))
	cfg = nc

	// 5. Optional restore, which re-checks the epoch rule itself.
	if o.FromBackup != "" {
		r := o.Restorer
		if r == nil {
			r = UnavailableRestorer{}
		}
		var sb strings.Builder
		rc := *cfg
		rc.DBPath = dbPath // the service's working directory, not this shell's
		if err := r.Restore(&rc, o.FromBackup, &sb, o.Now); err != nil {
			return lines, fmt.Errorf("restore: %w", err)
		}
		say("%s", strings.TrimSpace(sb.String()))
	}
	say("next: start the SiteControl service, then run `sitectl admin swap status -config %s -wait 30m` until it says READY", filepath.Base(o.CfgPath))
	return lines, nil
}

// FetchSnapshot reads a post's /debug/sync-status (unauthenticated JSON).
func FetchSnapshot(ctx context.Context, base string) (*repl.Snapshot, error) {
	var s repl.Snapshot
	if err := getJSON(ctx, strings.TrimSuffix(base, "/")+"/debug/sync-status", nil, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Readiness is the swap verdict: READY only when every configured peer has
// answered since the post started, has handed back this post's own earlier
// records (§8.6 latch), refuses no key either way, and every stream is "ok"
// with nothing left to pull. Each blocker is one sentence in words.
func Readiness(cfg *config.Config, s *repl.Snapshot) (bool, []string) {
	var b []string
	if s.SiteID != cfg.SiteID {
		return false, []string{fmt.Sprintf("the service at this address answers as %q, not %q", s.SiteID, cfg.SiteID)}
	}
	bySite := map[string]repl.PeerSnapshot{}
	for _, p := range s.Peers {
		bySite[p.SiteID] = p
	}
	streamsOf := map[string]int{}
	for _, st := range s.Streams {
		name := cfg.SiteName(st.SiteID)
		streamsOf[st.SiteID]++
		switch {
		case st.State != repl.StateOK:
			b = append(b, fmt.Sprintf("%s's records (epoch %d): %s", name, st.NodeEpoch, stateWords(st.State)))
		case st.Watermark < st.Head:
			b = append(b, fmt.Sprintf("still catching up on %s's records (epoch %d): %d of %d", name, st.NodeEpoch, st.Watermark, st.Head))
		}
	}
	for _, pc := range cfg.Peers {
		name := cfg.SiteName(pc.SiteID)
		p, ok := bySite[pc.SiteID]
		switch {
		case !ok || p.LastContactAgoSec == nil:
			b = append(b, fmt.Sprintf("%s has not answered since this post started — check the link to it", name))
			continue
		case p.LastError != "":
			b = append(b, fmt.Sprintf("%s: %s", name, errorKindWords(p.ErrorKind)))
		}
		if p.SelfHistoryPending {
			b = append(b, fmt.Sprintf("%s has not yet handed back this post's own earlier records", name))
		}
		if p.InboundKeyRefused || p.PokeRefused {
			b = append(b, fmt.Sprintf("%s and this post do not hold matching keys — compare the fingerprints in both files (`sitectl admin check`)", name))
		}
		if p.OwnCopyStuck {
			b = append(b, fmt.Sprintf("this post's new records are not reaching %s", name))
		}
		if streamsOf[pc.SiteID] == 0 {
			b = append(b, fmt.Sprintf("no records from %s have been confirmed yet", name))
		}
	}
	return len(b) == 0, b
}

func stateWords(st string) string {
	switch st {
	case repl.StateSchemaMismatch:
		return "refused — the two laptops run different SiteControl versions"
	case repl.StateIdentityRejected:
		return "refused — a record claims the wrong post"
	case repl.StateGapStalled:
		return "stalled on a gap in the records"
	case repl.StatePrefixDiverged:
		return "DIVERGED — that laptop was rebuilt without raising its epoch; run `swap plan` there"
	case repl.StateRegressed:
		return "that laptop's records went BACKWARDS — it was restored without raising its epoch"
	case repl.StateLeftBehind:
		return "another post holds MORE of this post's own records than this laptop does — run `swap plan` again"
	case repl.StateUnreported:
		return "not heard from yet"
	default:
		return "not synced (" + st + ")"
	}
}

func errorKindWords(k string) string {
	switch k {
	case repl.ErrorKindUnreachable:
		return "cannot be reached — check the link to it"
	case repl.ErrorKindKeyRejected:
		return "refuses this post's key"
	case repl.ErrorKindIdentityRejected:
		return "refuses this post's identity — it may not list this post as a peer"
	default:
		return "sync error"
	}
}

// SwapStatus polls the local post until READY or the wait runs out. It
// returns the final verdict lines and whether it was ready.
func SwapStatus(ctx context.Context, cfg *config.Config, wait, every time.Duration, progress func(string)) (bool, []string) {
	deadline := time.Now().Add(wait)
	for {
		snap, err := FetchSnapshot(ctx, LocalBase(cfg))
		var ready bool
		var lines []string
		if err != nil {
			lines = []string{fmt.Sprintf("this post's service at %s: %v — is it running?", LocalBase(cfg), err)}
		} else {
			ready, lines = Readiness(cfg, snap)
		}
		if ready || wait <= 0 || time.Now().After(deadline) {
			return ready, lines
		}
		if progress != nil {
			progress(fmt.Sprintf("NOT READY yet (%d blocker(s)); checking again in %s", len(lines), every))
		}
		select {
		case <-ctx.Done():
			return false, append(lines, "interrupted")
		case <-time.After(every):
		}
	}
}
