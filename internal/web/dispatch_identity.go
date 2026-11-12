// dispatch_identity.go — FIX11: on the dispatch host the page speaks as
// Dispatch, not as the post whose process serves it (James 2026-10-05: "the
// dispatch analytics page shows as 2P Ice Road Entrance on the top of the
// page"). The host is reverse-proxied to a post's process, so the shared top
// bar, the <title> and the "this post" wording all carried that post's name.
//
// One rewrite, at the writer, keyed on the same dispatchHost rule as the
// read-only guard and the Home link (homelink.go); no handler or template
// needs to know the host. Post hosts pass through untouched. Numbers are
// never altered: only the top bar's identity slot, the <title>, and the
// "this post" phrasing on the analytics pages.
package web

import (
	"bytes"
	"html"
	"net/http"
	"regexp"
	"strings"
)

const (
	dispatchName     = "Dispatch"
	dispatchLongName = "Dispatch — all sites"
)

var (
	topbarSiteRe = regexp.MustCompile(`(?s)(<div class="topbar-site" data-topbar-site>).*?(</div>)`)
	titleRe      = regexp.MustCompile(`(?s)<title>(.*?)</title>`)
	thisPostOfRe = regexp.MustCompile(`THIS post \(([^)]*)\)`)
)

// dispatchIdentity wraps the served surface. Off the dispatch host it is a
// pass-through.
func (s *Server) dispatchIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !dispatchHost(r.Host) {
			next.ServeHTTP(w, r)
			return
		}
		bw := &identityWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(bw, r)
		bw.finish(s.postName(), r.URL.Path)
	})
}

func (s *Server) postName() string {
	if s.Cfg == nil {
		return ""
	}
	return s.Cfg.DisplayName
}

// identityWriter buffers a response so it can be rewritten whole; only HTML
// is touched.
type identityWriter struct {
	http.ResponseWriter
	buf    bytes.Buffer
	status int
	wrote  bool
}

func (b *identityWriter) WriteHeader(code int) {
	if !b.wrote {
		b.status = code
		b.wrote = true
	}
}

func (b *identityWriter) Write(p []byte) (int, error) { return b.buf.Write(p) }

func (b *identityWriter) finish(post, path string) {
	body := b.buf.Bytes()
	h := b.Header()
	if strings.HasPrefix(h.Get("Content-Type"), "text/html") ||
		(h.Get("Content-Type") == "" && bytes.Contains(body, []byte("<html"))) {
		body = rewriteDispatchIdentity(body, post, path)
		h.Del("Content-Length")
	}
	b.ResponseWriter.WriteHeader(b.status)
	_, _ = b.ResponseWriter.Write(body)
}

func rewriteDispatchIdentity(body []byte, post, path string) []byte {
	body = topbarSiteRe.ReplaceAll(body, []byte(`${1}<span class="site-id">`+dispatchName+`</span><span class="site-name">`+dispatchLongName+`</span>${2}`))
	body = titleRe.ReplaceAllFunc(body, func(m []byte) []byte {
		t := titleRe.FindSubmatch(m)[1]
		return []byte("<title>" + dispatchTitle(string(t), post, path) + "</title>")
	})
	if strings.HasPrefix(path, "/analytics") && post != "" {
		name := html.EscapeString(post)
		s := thisPostOfRe.ReplaceAllString(string(body), "$1")
		s = strings.ReplaceAll(s, "</strong> (this post)", "</strong>")
		s = strings.ReplaceAll(s, "(this post)", "(serving post)")
		s = strings.ReplaceAll(s, "this post's", name+"'s")
		s = strings.ReplaceAll(s, "this post’s", name+"’s")
		s = strings.ReplaceAll(s, "THIS post", name)
		s = strings.ReplaceAll(s, "this post", name)
		body = []byte(s)
	}
	return body
}

// dispatchTitle drops the post's name from a title and makes sure it says
// Dispatch. /analytics/site/* titles name the site being viewed, which may
// be any site, so only the generic trailing parts are touched there.
func dispatchTitle(t, post, path string) string {
	if strings.HasPrefix(path, "/analytics/site/") {
		return t
	}
	var keep []string
	hasDispatch := false
	for _, seg := range strings.Split(t, " — ") {
		if seg == html.EscapeString(post) || seg == "Site Control" || seg == post {
			continue
		}
		if strings.HasPrefix(seg, dispatchName) {
			hasDispatch = true
		}
		keep = append(keep, seg)
	}
	if !hasDispatch {
		keep = append(keep, dispatchName)
	}
	return strings.Join(keep, " — ")
}
