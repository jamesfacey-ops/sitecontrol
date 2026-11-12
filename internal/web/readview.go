// readview.go — FIX17c (2026-10-07): one read snapshot per page, never
// queued behind a write.
//
// Every GET (and HEAD) runs inside a store read view (store/readpool.go):
// every read the page makes — its sections, the alert banner, the lost-signal
// strip and the other wrappers Root layers on — comes from ONE snapshot of
// the last committed state, taken at the page's first read, so a commit
// landing part-way through a render cannot tear it; and none of those reads
// waits for a write in progress, however long that write's fsync takes.
// /muster above all: the evacuation list keeps rendering while a write is
// stuck.
//
// The snapshot is released at the response's first byte, so a slow client
// never holds it open. A POST runs unbound: its reads after its own commit
// see that commit, and the GET it redirects to opens its snapshot after the
// commit returned — read-your-writes holds without routing anything to the
// writer.
package web

import "net/http"

func (s *Server) readViews(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Store == nil || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
			next.ServeHTTP(w, r)
			return
		}
		end := s.Store.ReadView()
		defer end()
		next.ServeHTTP(&snapshotReleasingWriter{ResponseWriter: w, end: end}, r)
	})
}

// snapshotReleasingWriter ends the page's read view when the response
// starts: everything the page reads is read by then.
type snapshotReleasingWriter struct {
	http.ResponseWriter
	end func()
}

func (w *snapshotReleasingWriter) WriteHeader(code int) {
	w.end()
	w.ResponseWriter.WriteHeader(code)
}

func (w *snapshotReleasingWriter) Write(p []byte) (int, error) {
	w.end()
	return w.ResponseWriter.Write(p)
}

func (w *snapshotReleasingWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
