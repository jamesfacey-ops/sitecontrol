//go:build sc_stallhook

// stallhook_debug.go — MEASUREMENT BUILDS ONLY (go build -tags sc_stallhook).
// Never in a product binary: the tag is not set by any build script.
//
// SC_DEBUG_COMMIT_STALL=<trigger file>:<milliseconds>. At each writer commit,
// if the trigger file exists it is removed and that one commit is held inside
// the commit window — writes complete, not yet visible, the writer held — for
// the given time: a commit stuck in its fsync, on demand, on a running post.
package store

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

func init() {
	spec := os.Getenv("SC_DEBUG_COMMIT_STALL")
	i := strings.LastIndex(spec, ":")
	if i <= 0 {
		return
	}
	trigger := spec[:i]
	ms, err := strconv.Atoi(spec[i+1:])
	if err != nil {
		return
	}
	log.Printf("store: DEBUG BUILD — a commit stalls %d ms whenever %s exists", ms, trigger)
	debugCommitStall = func() {
		if os.Remove(trigger) != nil {
			return
		}
		log.Printf("store: DEBUG commit stall: holding this commit %d ms", ms)
		time.Sleep(time.Duration(ms) * time.Millisecond)
		log.Printf("store: DEBUG commit stall: released")
	}
}
