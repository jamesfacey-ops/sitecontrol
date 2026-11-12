// homelink.go — FIX10: the top bar's Home button points at the landing page
// of whichever surface the operator is on. _topbar.html emits the post form
// (href="/gate"); this rewrites that one tag per response.
//
//   - post host: "/gate" — what "/" redirects to and where login lands.
//   - dispatch host (dispatchHost, the same rule as the read-only guard):
//     "/analytics", the only page that host serves besides search.
//
// aria-current="page" is added when the request is already at that landing
// page. A rewrite at the writer, like ThemeMiddleware, so no handler needs
// to know the host.
package web

import (
	"bytes"
	"net/http"
)

var homeTag = []byte(`<a class="home-btn" href="/gate" data-home>`)

func homeLink(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		landing := "/gate"
		if dispatchHost(r.Host) {
			landing = "/analytics"
		}
		var tag []byte
		tag = append(tag, `<a class="home-btn" href="`+landing+`" data-home`...)
		if r.URL.Path == landing {
			tag = append(tag, ` aria-current="page"`...)
		}
		tag = append(tag, '>')
		next.ServeHTTP(&homeWriter{ResponseWriter: w, tag: tag}, r)
	})
}

type homeWriter struct {
	http.ResponseWriter
	tag []byte
}

func (h *homeWriter) Write(p []byte) (int, error) {
	if !bytes.Contains(p, homeTag) {
		return h.ResponseWriter.Write(p)
	}
	if _, err := h.ResponseWriter.Write(bytes.Replace(p, homeTag, h.tag, 1)); err != nil {
		return 0, err
	}
	return len(p), nil
}
