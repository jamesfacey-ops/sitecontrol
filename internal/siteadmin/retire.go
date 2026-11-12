package siteadmin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"sitecontrol/internal/config"
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// "Delete a site" in an append-only, replicated system means RETIRE: the
// post stops authoring and leaves every peer list; its history stays in
// every replica that holds it (dumps must stay identical across posts, and
// other posts' events reference the people it enrolled). Two commands:
//
//   site retire  — at the RETIRING post, service stopped: proves nobody is
//                  on site there, no run to or from it is open, and every
//                  peer already holds its records to its final head; then
//                  writes leave-<id>.toml.
//   peer remove  — at every REMAINING post: re-checks its own view, then
//                  edits its file (ApplyLeave).

// OpenReadStore opens a post's database the way `sitectl -dump` does (a
// second reader beside a running service is safe: WAL), with every
// projection registered so domain reads work. A missing file refuses —
// opening must never create one.
func OpenReadStore(dbPath string) (*store.Store, error) {
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("no database at %s (pass -db if the service keeps it elsewhere): %w", dbPath, err)
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return nil, err
	}
	if err := domain.Register(st); err != nil {
		st.Close()
		return nil, err
	}
	if err := deadline.Register(st); err != nil {
		st.Close()
		return nil, err
	}
	return st, nil
}

// OpenWork is what still ties people or trucks to a post: open visits AT
// it and open runs to or from it, as one store sees them.
type OpenWork struct {
	OnSite   int
	RunsTo   int
	RunsFrom int
	RoadWork int
}

func (w OpenWork) Empty() bool { return w.OnSite+w.RunsTo+w.RunsFrom+w.RoadWork == 0 }

func (w OpenWork) Sentences(name string) []string {
	var s []string
	if w.OnSite > 0 {
		s = append(s, fmt.Sprintf("%d person(s) are still on site at %s — check them out there (the muster page lists them) before it retires", w.OnSite, name))
	}
	if w.RunsTo > 0 {
		s = append(s, fmt.Sprintf("%d run(s) to %s are still open — record their arrival or cancel them first, or they go overdue with nobody to close them", w.RunsTo, name))
	}
	if w.RunsFrom > 0 {
		s = append(s, fmt.Sprintf("%d run(s) from %s are still open — record their arrival or cancel them first", w.RunsFrom, name))
	}
	if w.RoadWork > 0 {
		s = append(s, fmt.Sprintf("%d vehicle(s) sent out working on the road from %s are still out — record their return first", w.RoadWork, name))
	}
	return s
}

// OpenWorkAt counts siteID's open work in st.
func OpenWorkAt(st *store.Store, siteID string) (OpenWork, error) {
	var w OpenWork
	v, err := domain.OpenVisitsAt(st, siteID)
	if err != nil {
		return w, err
	}
	to, err := domain.OpenExpectations(st, siteID)
	if err != nil {
		return w, err
	}
	from, err := domain.OpenOutbound(st, siteID)
	if err != nil {
		return w, err
	}
	rw, err := domain.OpenRoadWork(st, siteID)
	if err != nil {
		return w, err
	}
	w.OnSite, w.RunsTo, w.RunsFrom, w.RoadWork = len(v), len(to), len(from), len(rw)
	return w, nil
}

// OwnHeads is siteID's head in every epoch st holds.
func OwnHeads(st *store.Store, siteID string) ([]FinalHead, error) {
	eps, err := st.Epochs(siteID)
	if err != nil {
		return nil, err
	}
	var out []FinalHead
	for _, e := range eps {
		h, err := st.Watermark(siteID, e)
		if err != nil {
			return nil, err
		}
		out = append(out, FinalHead{Epoch: e, Head: h})
	}
	return out, nil
}

// RetireOptions are `site retire`'s flags.
type RetireOptions struct {
	CfgPath    string
	DBOverride string
	OutDir     string
}

// Retire runs at the retiring post. Every refusal leaves nothing written.
func Retire(ctx context.Context, o RetireOptions) ([]string, error) {
	cfg, err := config.Load(o.CfgPath)
	if err != nil {
		return nil, err
	}
	name := cfg.PostName()
	if Answers(ctx, LocalBase(cfg)) {
		return nil, fmt.Errorf("REFUSED: %s's service still answers at %s — once nobody is on site and no run is open, stop the service, then run this again (a running post could record something after the check)", name, LocalBase(cfg))
	}
	st, err := OpenReadStore(ResolveDBPath(o.CfgPath, cfg, o.DBOverride))
	if err != nil {
		return nil, err
	}
	defer st.Close()
	var probs []string
	w, err := OpenWorkAt(st, cfg.SiteID)
	if err != nil {
		return nil, err
	}
	probs = append(probs, w.Sentences(name)...)
	heads, err := OwnHeads(st, cfg.SiteID)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, p := range cfg.Peers {
		pn := cfg.SiteName(p.SiteID)
		snap, err := FetchSnapshot(ctx, p.BaseURL)
		if err != nil {
			probs = append(probs, fmt.Sprintf("%s: %v — cannot prove it holds all of %s's records", pn, err, name))
			continue
		}
		held := map[int64]int64{}
		for _, s := range snap.Streams {
			if s.SiteID == cfg.SiteID {
				held[s.NodeEpoch] = s.Watermark
			}
		}
		short := false
		for _, h := range heads {
			if h.Head > 0 && held[h.Epoch] < h.Head {
				short = true
				probs = append(probs, fmt.Sprintf("%s holds %d of %s's %d record(s) in epoch %d — start %s's service again until it catches up, then stop it and re-run", pn, held[h.Epoch], name, h.Head, h.Epoch, name))
			}
		}
		if !short {
			lines = append(lines, fmt.Sprintf("  %-24s holds every record of %s", pn, name))
		}
	}
	if len(probs) > 0 {
		return nil, refusal(probs)
	}
	if ok, err := dirEmptyOrAbsent(o.OutDir); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("REFUSED: %s is not empty — use a new, empty folder", o.OutDir)
	}
	if err := os.MkdirAll(o.OutDir, 0o700); err != nil {
		return nil, err
	}
	frag := LeaveFragment{Kind: "leave", SiteID: cfg.SiteID, DisplayName: cfg.PostName(), LongName: cfg.LongName, FinalHeads: heads}
	path := filepath.Join(o.OutDir, "leave-"+cfg.SiteID+".toml")
	if err := WriteSecretFile(path, []byte(renderLeave(frag))); err != nil {
		return nil, err
	}
	lines = append(lines,
		fmt.Sprintf("  nobody on site, no open run, every peer holds %s's records: %s may retire", name, name),
		fmt.Sprintf("  wrote %s — at every remaining post: sitectl admin peer remove -config site.toml %s, then restart it", path, filepath.Base(path)),
		fmt.Sprintf("  then: take a final backup of %s's database and keep it; remove %s from every firewall allow-list; wipe this laptop", name, name))
	return lines, nil
}

// PeerRemoveCheck is the remaining post's own re-check before its file is
// edited: by ITS records, nobody is on site at the retiring post, no run to
// or from it is open, and it holds the retiring post's final heads.
func PeerRemoveCheck(st *store.Store, cfg *config.Config, f *LeaveFragment) []string {
	name := cfg.SiteName(f.SiteID)
	if f.DisplayName != "" {
		name = f.DisplayName
	}
	var probs []string
	w, err := OpenWorkAt(st, f.SiteID)
	if err != nil {
		return []string{err.Error()}
	}
	probs = append(probs, w.Sentences(name)...)
	for _, h := range f.FinalHeads {
		got, err := st.Watermark(f.SiteID, h.Epoch)
		if err != nil {
			return []string{err.Error()}
		}
		if got < h.Head {
			probs = append(probs, fmt.Sprintf("this post holds %d of %s's %d record(s) in epoch %d — it must catch up before %s is removed (start %s again briefly, or wait for sync)", got, name, h.Head, h.Epoch, name, name))
		}
	}
	return probs
}
