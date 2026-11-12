// sitectl is the one binary (chunk-plan §6). Instances are differentiated
// solely by the per-site config file passed via -config.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/repl"
	"sitecontrol/internal/store"
	"sitecontrol/internal/web"
)

// defaultMemoryLimit is the Go runtime's soft memory limit for a post when
// the environment sets none (FIX17b, memory). MEASURED on scratch copies of
// the staging season (npf, Ice Pad, NDB): live heap with every page's reads
// memoised is ~70-80 MB; without a limit the collector let the heap run to
// twice that and RSS sat at 200-230 MB (sc-stress: 221-278 MB per post).
// With 100 MiB, RSS settles at 96-117 MB (peak 136 MB while the whole-
// season visits report page renders) and the officer pages keep their
// speed. It is SOFT: a post whose live data outgrows it is not stopped —
// the collector just runs more often (the runtime caps that at half a CPU).
// GOMEMLIMIT in the service's environment overrides it ("off" removes it).
const defaultMemoryLimit = 100 << 20

func main() {
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(defaultMemoryLimit)
	}
	// `sitectl admin …` (FIX16 H) is the IT tool, a separate command line;
	// see admin.go. Checked before any flag is parsed.
	if len(os.Args) > 1 && os.Args[1] == "admin" {
		os.Exit(runAdmin(os.Args[2:], os.Stdout, os.Stderr))
	}
	configPath := flag.String("config", "", "path to per-site TOML config (required)")
	dump := flag.Bool("dump", false, "write this post's canonical projection dump to stdout and exit, without serving anything")
	report := flag.String("report", "", "write a read-only CSV report to stdout and exit, without serving anything; the reports are 'visits' (per-visit time on site and how each visit closed, derived from event_log) and 'signout-failures' (per person: visits closed by the person's own return with no departure recorded, at a text-message site — R-4; camp sites exempt — R-5)")
	backupTo := flag.String("backup", "", "take one verified backup copy of this post's database into the given folder and exit (safe while the post is running; keeps the 7 newest copies there). The running post also takes one nightly by itself — see backup_dir / backup_time")
	restoreFrom := flag.String("restore", "", "replace this post's database with the given backup copy and exit; refuses while the post is running, verifies the copy first, and requires node_epoch above every epoch the post has authored in (deploy/BACKUP-RESTORE.md)")
	flag.Parse()
	if *configPath == "" {
		log.Fatal("sitectl: -config is required (one binary, config-only differentiation)")
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("sitectl: %v", err)
	}

	// -backup / -restore (FIX16 D) run BEFORE store.Open: -backup reads the
	// file through its own connection (store.BackupFile), and -restore must
	// never let Open create or touch the database it is about to replace.
	if *backupTo != "" {
		res, err := store.TakeBackup(cfg.DBPath, store.BackupDirs{Primary: *backupTo}, time.Now())
		if err != nil {
			log.Fatalf("sitectl: backup FAILED: %v", err)
		}
		log.Printf("sitectl: backup: verified copy %s (%s)", res.Path, res.Took.Round(time.Millisecond))
		return
	}
	if *restoreFrom != "" {
		if err := restoreBackup(cfg, *restoreFrom, os.Stdout, time.Now()); err != nil {
			log.Fatalf("sitectl: %v", err)
		}
		return
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("sitectl: %v", err)
	}
	defer st.Close()

	// Reference-data projections (C5) register at the composition root, not inside
	// store.Open: store cannot import domain (domain imports store for Envelope /
	// Projection, so registering there would be an import cycle), and
	// RegisterProjection rejects duplicate names, so C6's real trip fold could not
	// coexist with a built-in. This is the single registration site.
	if err := domain.Register(st); err != nil {
		log.Fatalf("sitectl: %v", err)
	}

	// C7's replicated projection (the alert_acknowledged fold) registers
	// alongside domain's, at the same composition root.
	if err := deadline.Register(st); err != nil {
		log.Fatalf("sitectl: %v", err)
	}

	// -dump: the two-post determinism comparison's ONLY read path (D-56).
	//
	// GET /debug/projection-dump used to serve these same bytes over HTTP,
	// unauthenticated and in every shipped configuration, which made a
	// verification instrument into an operational data-dump surface — the
	// thing D-56 forbids. The route is gone; the capability is not. It moved
	// here, where it costs an operator who ALREADY has a shell on the post
	// (the same access that lets them read site-<x>.db with sqlite3, so this
	// adds no reach) and costs a network caller everything.
	//
	// It must run AFTER both Register calls and BEFORE anything is served:
	// the dump is only meaningful over the full registered projection set,
	// and byte-identity between two posts is only meaningful if both dumps
	// come from the same binary at the same SchemaVersion — which store.Open
	// above has already refused to violate. DumpProjections reads every table
	// in one transaction, so this is a single consistent snapshot and is safe
	// to take while the service is running (WAL, second reader).
	//
	// Nothing is filtered, redacted or reordered on the way out. That is
	// deliberate and load-bearing: a column suppressed from the dump is a
	// column whose cross-site divergence becomes invisible to the only check
	// that would catch it (W5b/W5e build critiques).
	if *dump {
		b, err := st.DumpProjections()
		if err != nil {
			log.Fatalf("sitectl: dump: %v", err)
		}
		if _, err := os.Stdout.Write(b); err != nil {
			log.Fatalf("sitectl: dump: %v", err)
		}
		return
	}

	// -report visits: the analytics MVP (domain/visit_report.go). Same
	// operator-with-a-shell posture as -dump, for the same reason (D-56): it
	// reads event_log, which the dump deliberately does not carry. CSV on
	// stdout only here; the SAME rows are also served, behind the login, as
	// /reports/<name>(.csv) (FIX16 stream B, James-approved 2026-10-06:
	// "expose the existing sitectl -report … as web pages or downloads").
	// ⟦was: "and it must never become a network surface"⟧ The denominator
	// lines (visits examined, persons after merge resolution, unattributable
	// rows, per-stream coverage) go to stderr through the logger so a
	// redirected stdout stays pure CSV. Read-only: it authors nothing. Runs
	// after both Register calls for the same reason -dump does, and before
	// anything is served.
	if *report != "" {
		if !domain.IsReportName(*report) {
			log.Fatalf("sitectl: -report %q: unknown report (the reports are 'visits' and 'signout-failures')", *report)
		}
		// This post knows ONE site type — its own, from config (design §7:
		// config is local-only). Peer sites' rows are emitted with no
		// verdict and named on stderr; run the report at that post for its
		// verdicts. domain.WriteReport is the SAME call the web's
		// /reports/<name>.csv makes (FIX16), so the two cannot differ.
		types := domain.SiteTypes{cfg.SiteID: cfg.SiteType}
		sum, err := domain.WriteReport(os.Stdout, st, *report, types)
		if err != nil {
			log.Fatalf("sitectl: report %s: %v", *report, err)
		}
		for _, line := range sum.Lines() {
			log.Printf("sitectl: report %s: %s", *report, line)
		}
		return
	}

	// Windows service mode: svc.IsWindowsService() distinguishes "launched by
	// the Service Control Manager" from "launched from a console" with no
	// new flag and no config/wire change. isWindowsService/runAsService are
	// implemented per-GOOS (service_windows.go / service_other.go) so this
	// file carries no build tag; on every non-Windows target isSvc is always
	// false and runAsService is unreachable.
	isSvc, err := isWindowsService()
	if err != nil {
		log.Fatalf("sitectl: %v", err)
	}
	if isSvc {
		if err := runAsService(cfg, st); err != nil {
			log.Fatalf("sitectl: %v", err)
		}
		return
	}

	// Interactive mode (console, or any non-service launch on any platform):
	// Ctrl+C / SIGTERM cancels ctx, which runServer treats identically to a
	// Windows service Stop request — one shutdown path for both.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runServer(ctx, cfg, st); err != nil {
		log.Fatalf("sitectl: %v", err)
	}
}

// runServer assembles the replication engine, deadline-alert scanner and HTTP
// server for cfg/st, runs them until ctx is cancelled, then shuts down
// gracefully: the HTTP server stops accepting new connections and drains
// in-flight requests (up to 10s) before returning, and engine.Run/alerts.Run
// (both already ctx-aware) stop their loops on the same signal. The caller's
// deferred st.Close() then runs against a quiesced store. Shared by
// interactive mode (ctx cancelled on SIGINT/SIGTERM, above) and Windows
// service mode (ctx cancelled on a Stop/Shutdown control request, see
// service_windows.go) so both paths shut down the same way.
func runServer(ctx context.Context, cfg *config.Config, st *store.Store) error {
	// The replication engine (C3a): serves this site's log at /sync/ and
	// pulls every configured peer on the routine timer. The §8.9 fast path —
	// authored events POKE peers, records never pushed — is wired through
	// the store, not through engine.Author: every door authors via
	// store.Author (alone, or inside a Transact commit), and OnAuthored fires
	// once per COMMITTED authoring transaction, after the commit, on the
	// authoring goroutine; PokePeers returns at once and pokes asynchronously
	// (one in flight + one pending per peer), so a slow or dead peer never
	// delays an officer's write. ⟦W7 / Y-17, 2026-10-03⟧ Until today this
	// comment claimed the poke ran "via engine.Author", which nothing in the
	// product called — the poke was dead and replication was tick-only
	// (3-17 s single-write latency on the sc-test L12 lane). Best-effort by
	// ruling (FIX-PLAN §3): a failed poke is recorded on the status surface,
	// never retried; the routine tick remains the backstop, unchanged.
	engine := repl.New(cfg, st)
	st.OnAuthored = engine.PokePeers

	// C7: the deadline machinery. The capture hook MUST be wired before any
	// pulling starts (engine.OnApplied's contract): it fires synchronously
	// per applied envelope with the pre-rotation §9.3 anchor, and persists
	// (receipt wall-time, remaining-at-receipt) for every inbound deadline
	// trip — the anchor's currency is drain-relative, so capturing later is
	// structurally wrong, not merely late.
	alerts, err := deadline.New(st, cfg.SiteID, cfg.SyncInterval())
	if err != nil {
		log.Fatalf("sitectl: %v", err)
	}
	engine.OnApplied = alerts.OnApplied
	// R9 (James, 2026-10-04 — FINDINGS-R4 D5): the boot clock beside the
	// wall clock, so a clock STEP across a restart (or under an own-authored
	// run's bridge) sends the trip to "clock changed since receipt — verify
	// by radio" instead of moving its countdown by the step.
	alerts.Uptime = deadline.HostUptime
	alerts.Staleness = func(origin string) (time.Duration, bool) {
		for _, p := range engine.Snapshot().Peers {
			if p.SiteID == origin && p.LastContactAgoSec != nil {
				return time.Duration(*p.LastContactAgoSec * float64(time.Second)), true
			}
		}
		return 0, false // not heard since this process started — see ProcessUptime
	}
	alerts.ProcessUptime = engine.Uptime
	// ⟦was: the weather-phase wiring — alerts.CurrentPhase (the phase fold),
	// TravelMinutes (cfg.SegmentMinutes), ReviewInTransit and PhaseDisplayName.
	// Weather phases removed, James 2026-10-04: the scan's allowance is the
	// officer's recorded deadline, never rescaled.⟧

	// Rejoin (C3b) runs SYNCHRONOUSLY, before the routine timer starts. That ordering is
	// the point, not an optimisation: 8.6 requires this node's own prior-epoch history to
	// be recovered before any peer log, and starting Run first would make own-history-first
	// merely eventual rather than observed. Idempotent and safe unconditionally — on a node
	// with nothing to recover it is a no-op that still catches up peer logs.
	// C7's replay gate is raised around it (§9.4): any scan that runs while
	// this from-zero replay is in progress classifies every open expectation
	// overdue-unresolved, and the captures it mints are anchorless (no
	// pre-existing drain), so they stay in the review lane after the gate
	// drops — a replayed partition-expired trip cannot resurface live.
	// A boot-time recovery FAILURE (e.g. the link is down at install time)
	// is not final: Rejoin arms a per-peer latch, the routine pull path
	// retries the self-history drain every sync cycle until it verifiably
	// completes, and the interim is surfaced as self_history_pending on
	// /debug/sync-status and as a not-healthy peer row on the status page.
	alerts.SetReplaying(true)
	engine.Rejoin(ctx)
	alerts.SetReplaying(false)

	go engine.Run(ctx)
	go alerts.Run(ctx) // the §9.4 periodic server-side scan; both loops already select on ctx.Done and return when it fires

	// FIX16 D: the nightly backup, in-process — no OS scheduler on either
	// platform. Its own connection, one read snapshot; never the store's
	// writer connection.
	backups := newBackupScheduler(cfg)
	go backups.run(ctx)

	srv := web.New(cfg, st)
	srv.Backup = backups.facts
	srv.Sync = engine.SyncHandler()
	srv.SyncStatus = engine.StatusHandler()
	srv.Alerts = alerts // every page renders the banner from the DB scan
	// C10: the status page's sync-status section reads the SAME snapshot
	// /debug/sync-status serves, via a typed accessor rather than a second
	// opaque handler (web.go's SyncSnapshot doc comment explains why).
	srv.SyncSnapshot = engine.Snapshot
	// FIX16f: the lost-signal history — a site-local table (never folded,
	// replicated or dumped; no SchemaVersion bump) and a recorder that
	// samples the same snapshot every sync interval (web/signal_history.go).
	if err := web.RegisterSignalHistory(st); err != nil {
		log.Fatalf("sitectl: %v", err)
	}
	go srv.RunSignalRecorder(ctx)
	log.Printf("sitectl: site=%s epoch=%d db=%s listening on %s (sync every %s, %d peer(s))",
		cfg.SiteID, cfg.NodeEpoch, cfg.DBPath, cfg.ListenAddr, cfg.SyncInterval(), len(cfg.Peers))

	// web.ThemeMiddleware applies the light/dark switch's cookie to every
	// served page; it is layered on here, outside srv.Handler(), rather
	// than inside it, because several web-package tests type-assert
	// srv.Handler()'s return value to *http.ServeMux directly (see
	// theme.go's doc comment on ThemeMiddleware). srv.Root() is that
	// composition plus the shared refusal page (web/refusal.go): a bare
	// text error on an officer route is served as a full page.
	httpSrv := &http.Server{Addr: cfg.ListenAddr, Handler: srv.Root()}
	serveErr := make(chan error, 1)
	go func() { serveErr <- httpSrv.ListenAndServe() }()

	select {
	case err := <-serveErr:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	case <-ctx.Done():
		// Graceful: stop accepting new connections, let in-flight ones
		// finish (bounded so a stuck request can't hang a service Stop
		// forever), then wait for the ListenAndServe goroutine above to
		// actually return before this function returns — so the caller's
		// deferred st.Close() never races a request still touching the DB.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		<-serveErr
		return nil
	}
}
