//go:build windows

package main

import (
	"context"
	"log"
	"time"

	"golang.org/x/sys/windows/svc"

	"sitecontrol/internal/config"
	"sitecontrol/internal/store"
)

// windowsServiceName must match the name the install script registers with
// the Service Control Manager (deploy/windows/install-service.ps1). It is
// only used here for the SCM handshake and event-log source name — there is
// one sitectl instance per host, so a fixed name is enough; a multi-instance
// host would need this made configurable (e.g. a -service-name flag), which
// is out of scope for now.
const windowsServiceName = "SiteControl"

// isWindowsService reports whether this process was launched by the SCM
// (sc.exe start / a boot-time automatic start) rather than from a console.
// This is what fixes Windows error 1053 ("the service did not respond in a
// timely fashion"): 1053 is the classic symptom of a binary that, when
// launched by the SCM, never calls into the service API at all and instead
// just runs as if interactive, so the SCM's start request times out waiting
// for a status update that never comes. Checking this lets main.go branch
// into the real svc.Run handshake instead.
func isWindowsService() (bool, error) {
	return svc.IsWindowsService()
}

// sitectlService adapts runServer's ctx-cancel shutdown contract to the
// SCM's control-request protocol.
type sitectlService struct {
	cfg *config.Config
	st  *store.Store
}

// Execute is svc.Handler's entry point: the SCM calls it once, on its own
// goroutine, after svc.Run registers with StartServiceCtrlDispatcher. It
// must report StartPending/Running promptly (that promptness is exactly what
// 1053 checks for), then service Stop/Shutdown requests until told to exit.
func (s *sitectlService) Execute(_ []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (svcSpecificEC bool, exitCode uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown

	changes <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- runServer(ctx, s.cfg, s.st) }()

	changes <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case err := <-done:
			// runServer returned on its own (e.g. a listen error), not
			// because we cancelled it. Report Stopped with a non-zero
			// service-specific exit code so the SCM's configured recovery
			// action (restart) fires — the service equivalent of
			// interactive mode's log.Fatalf exit.
			if err != nil {
				log.Printf("sitectl: service: %v", err)
				changes <- svc.Status{State: svc.Stopped}
				return true, 1
			}
			changes <- svc.Status{State: svc.Stopped}
			return false, 0

		case req := <-r:
			switch req.Cmd {
			case svc.Interrogate:
				changes <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				cancel() // runServer drains the HTTP server and stops the sync/alert loops
				select {
				case err := <-done:
					if err != nil {
						log.Printf("sitectl: service: shutdown: %v", err)
					}
				case <-time.After(15 * time.Second):
					// Belt-and-braces: runServer's own shutdown timeout is
					// 10s, so this should never fire. If it does, report
					// Stopped anyway rather than hang the SCM's stop request
					// (and the pending st.Close() in main's defer, which
					// never runs on this path since the process is about to
					// exit via the SCM tearing it down).
					log.Printf("sitectl: service: shutdown: runServer did not return within 15s")
				}
				changes <- svc.Status{State: svc.Stopped}
				return false, 0
			}
		}
	}
}

// runAsService blocks running cfg/st under the SCM until a Stop/Shutdown
// request completes a graceful drain (see sitectlService.Execute), mirroring
// the interactive path's signal-driven runServer call in main().
func runAsService(cfg *config.Config, st *store.Store) error {
	return svc.Run(windowsServiceName, &sitectlService{cfg: cfg, st: st})
}
