package main

// `sitectl admin …` (FIX16 H): the IT tool — season setup, adding a post,
// swapping a post's laptop, retiring a post. The work is in
// internal/siteadmin; this file is only the command line. Design:
// fork-docs/SITE-TOOL-DESIGN.md. It is never a network surface and never
// runs inside a serving post.

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/siteadmin"
)

const adminUsage = `sitectl admin — set up, extend, swap and retire posts (run by IT, never by the service)

  sitectl admin season -plan site-plan.toml -out DIR
        every post's site.toml + passwords.txt + IT-CHECKLIST.txt from one plan; refuses unless the peers match up
  sitectl admin check site.toml site.toml ...
        do these posts' files match up? (each lists every other, keys held = keys accepted, addresses agree)
  sitectl admin site add -config LIVE.toml -self-url URL -new new-post.toml -out DIR
        add one post to a running mesh: its site.toml + join-<id>.toml for every existing post
  sitectl admin peer apply -config site.toml join-<id>.toml
        at each existing post: merge a join fragment (old file kept as site.toml.bak-…)
  sitectl admin swap plan -config site.toml [-old-url URL] [-from-backup FILE]
        on a REPLACEMENT laptop, before first start: raise node_epoch above every epoch held anywhere
  sitectl admin swap status -config site.toml [-wait 30m]
        READY / NOT READY: has the post pulled its own history and every peer's back? exit 0 = ready
  sitectl admin site retire -config site.toml -out DIR
        at the RETIRING post, service stopped: prove it is clear and fully replicated; writes leave-<id>.toml
  sitectl admin peer remove -config site.toml leave-<id>.toml
        at each remaining post: drop the retired post from peers and destinations, keep its name

Keys and passwords are never printed; files name a key only by its fingerprint (fp:…).
`

// runAdmin is `sitectl admin <args>`; it returns the process exit code.
func runAdmin(args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if len(args) == 0 || args[0] == "-h" || args[0] == "help" || args[0] == "--help" {
		fmt.Fprint(stdout, adminUsage)
		return 0
	}
	verb := args[0]
	rest := args[1:]
	if (verb == "site" || verb == "peer" || verb == "swap") && len(rest) > 0 {
		verb += " " + rest[0]
		rest = rest[1:]
	}
	fail := func(err error) int {
		fmt.Fprintln(stderr, "sitectl admin "+verb+": "+err.Error())
		return 1
	}
	printLines := func(lines []string) {
		for _, l := range lines {
			fmt.Fprintln(stdout, l)
		}
	}
	fs := flag.NewFlagSet("sitectl admin "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfgPath := fs.String("config", "", "this post's site.toml")
	out := fs.String("out", "", "output folder (must be new or empty)")
	db := fs.String("db", "", "this post's database, if not db_path beside site.toml")
	switch verb {
	case "season":
		plan := fs.String("plan", "", "site-plan.toml")
		if fs.Parse(rest) != nil || *plan == "" || *out == "" {
			return usageErr(stderr, "season needs -plan and -out")
		}
		p, err := siteadmin.LoadPlan(*plan)
		if err != nil {
			return fail(err)
		}
		res, err := siteadmin.Season(p, *out)
		if err != nil {
			return fail(err)
		}
		printLines(res.Lines)
		return 0

	case "check":
		if fs.Parse(rest) != nil || fs.NArg() < 2 {
			return usageErr(stderr, "check needs two or more site.toml files")
		}
		cfgs := map[string]*config.Config{}
		for _, p := range fs.Args() {
			c, err := config.Load(p)
			if err != nil {
				return fail(err)
			}
			if _, dup := cfgs[c.SiteID]; dup {
				return fail(fmt.Errorf("two files are both %s", c.SiteID))
			}
			cfgs[c.SiteID] = c
		}
		if probs := siteadmin.CrossCheck(cfgs); len(probs) > 0 {
			fmt.Fprintln(stdout, "DO NOT MATCH:")
			for _, p := range probs {
				fmt.Fprintln(stdout, "  - "+p)
			}
			return 1
		}
		ids := make([]string, 0, len(cfgs))
		for id := range cfgs {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			fmt.Fprintf(stdout, "  %-12s accepts %s\n", id, siteadmin.Fingerprint(cfgs[id].PeerKey))
		}
		fmt.Fprintln(stdout, "MATCH: every post lists every other, every key held is the key accepted, every address agrees")
		return 0

	case "site add":
		selfURL := fs.String("self-url", "", "the base_url the other posts use for the -config post")
		newPost := fs.String("new", "", "a plan file with exactly one [[post]] (the newcomer) and its [[route]]s")
		if fs.Parse(rest) != nil || *cfgPath == "" || *newPost == "" || *out == "" {
			return usageErr(stderr, "site add needs -config, -self-url, -new and -out")
		}
		live, err := os.ReadFile(*cfgPath)
		if err != nil {
			return fail(err)
		}
		p, err := siteadmin.LoadPlan(*newPost)
		if err != nil {
			return fail(err)
		}
		res, err := siteadmin.AddSite(live, *selfURL, p, *out)
		if err != nil {
			return fail(err)
		}
		printLines(res.Lines)
		return 0

	case "peer apply", "peer remove":
		if fs.Parse(rest) != nil || *cfgPath == "" || fs.NArg() != 1 {
			return usageErr(stderr, verb+" needs -config and one fragment file")
		}
		raw, err := os.ReadFile(*cfgPath)
		if err != nil {
			return fail(err)
		}
		var text string
		var notes []string
		if verb == "peer apply" {
			f, err := siteadmin.LoadJoin(fs.Arg(0))
			if err != nil {
				return fail(err)
			}
			if text, notes, err = siteadmin.ApplyJoin(string(raw), f); err != nil {
				return fail(err)
			}
		} else {
			f, err := siteadmin.LoadLeave(fs.Arg(0))
			if err != nil {
				return fail(err)
			}
			cfg, err := config.Parse(raw, *cfgPath)
			if err != nil {
				return fail(err)
			}
			st, err := siteadmin.OpenReadStore(siteadmin.ResolveDBPath(*cfgPath, cfg, *db))
			if err != nil {
				return fail(err)
			}
			probs := siteadmin.PeerRemoveCheck(st, cfg, f)
			st.Close()
			if len(probs) > 0 {
				return fail(fmt.Errorf("REFUSED — nothing was changed:\n  - %s", strings.Join(probs, "\n  - ")))
			}
			if text, notes, err = siteadmin.ApplyLeave(string(raw), f); err != nil {
				return fail(err)
			}
		}
		bak, err := siteadmin.BackupThenWrite(*cfgPath, []byte(text), time.Now())
		if err != nil {
			return fail(err)
		}
		for _, n := range notes {
			fmt.Fprintln(stdout, "  "+n)
		}
		fmt.Fprintf(stdout, "  old file kept as %s — restart the SiteControl service to use the new one\n", bak)
		return 0

	case "swap plan":
		o := siteadmin.SwapOptions{Restorer: adminRestorer, Now: time.Now()}
		fs.StringVar(&o.OldURL, "old-url", "", "the old laptop's address, if not this post's listen_addr")
		fs.StringVar(&o.FromBackup, "from-backup", "", "restore this backup after raising node_epoch")
		fs.Int64Var(&o.ForceEpoch, "epoch", 0, "with -unreachable-ok: the epoch to use")
		fs.BoolVar(&o.UnreachableOK, "unreachable-ok", false, "proceed although some peers did not answer")
		if fs.Parse(rest) != nil || *cfgPath == "" {
			return usageErr(stderr, "swap plan needs -config")
		}
		o.CfgPath, o.DBOverride = *cfgPath, *db
		lines, err := siteadmin.SwapPlan(ctx, o)
		printLines(lines)
		if err != nil {
			return fail(err)
		}
		return 0

	case "swap status":
		wait := fs.Duration("wait", 0, "keep checking until READY or this long has passed")
		if fs.Parse(rest) != nil || *cfgPath == "" {
			return usageErr(stderr, "swap status needs -config")
		}
		cfg, err := config.Load(*cfgPath)
		if err != nil {
			return fail(err)
		}
		ready, lines := siteadmin.SwapStatus(ctx, cfg, *wait, 15*time.Second, func(s string) { fmt.Fprintln(stderr, s) })
		if ready {
			fmt.Fprintf(stdout, "READY: %s has its own history back and is in step with every peer\n", cfg.PostName())
			return 0
		}
		fmt.Fprintf(stdout, "NOT READY: %s\n", cfg.PostName())
		for _, l := range lines {
			fmt.Fprintln(stdout, "  - "+l)
		}
		return 1

	case "site retire":
		if fs.Parse(rest) != nil || *cfgPath == "" || *out == "" {
			return usageErr(stderr, "site retire needs -config and -out")
		}
		lines, err := siteadmin.Retire(ctx, siteadmin.RetireOptions{CfgPath: *cfgPath, DBOverride: *db, OutDir: *out})
		if err != nil {
			return fail(err)
		}
		printLines(lines)
		return 0
	}
	fmt.Fprintf(stderr, "sitectl admin: unknown command %q\n\n%s", verb, adminUsage)
	return 2
}

func usageErr(w io.Writer, msg string) int {
	fmt.Fprintf(w, "sitectl admin: %s\n\n%s", msg, adminUsage)
	return 2
}

// adminRestorer is the swap's restore step: FIX16 D's -restore (backup.go),
// which re-checks everything itself (not running, copy verified, epoch rule).
var adminRestorer siteadmin.Restorer = siteadmin.RestorerFunc(restoreBackup)
