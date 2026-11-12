package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func loginBody(t *testing.T, srv *Server) string {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/login", nil))
	if rec.Code != 200 {
		t.Fatalf("GET /login = %d", rec.Code)
	}
	return rec.Body.String()
}

func TestLoginHintRenderedOnlyWhenSet(t *testing.T) {
	srv := newTestServer(t)
	if b := loginBody(t, srv); strings.Contains(b, "Demo login:") {
		t.Fatal("login page shows a Demo login line with login_hint unset")
	}
	srv.Cfg.LoginHint = "user demo / pass <b>x</b>"
	b := loginBody(t, srv)
	if !strings.Contains(b, "Demo login: user demo / pass &lt;b&gt;x&lt;/b&gt;") {
		t.Fatalf("hint missing or unescaped:\n%s", b)
	}
}
