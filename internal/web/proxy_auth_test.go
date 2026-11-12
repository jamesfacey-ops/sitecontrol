// proxy_auth_test.go — the demo's single-login path (X-SC-Proxy-Auth).
package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testProxyToken = "proxy-token-for-tests"

func getWithProxyHeader(srv *Server, path, header string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	if header != "" {
		req.Header.Set("X-SC-Proxy-Auth", header)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestProxyAuthTokenAcceptedServesPageWithNoLogin(t *testing.T) {
	srv, _ := newAuthTestServer(t)
	srv.Cfg.ProxyAuthToken = testProxyToken
	rec := getWithProxyHeader(srv, "/analytics", testProxyToken)
	if rec.Code != 200 || strings.Contains(rec.Body.String(), `name="password"`) {
		t.Fatalf("code=%d, login form served despite a valid proxy token", rec.Code)
	}
	// A normal session was minted: its cookie alone now opens a gated page.
	var ck *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			ck = c
		}
	}
	if ck == nil {
		t.Fatal("no session cookie minted")
	}
	req := httptest.NewRequest("GET", "/analytics", nil)
	req.AddCookie(ck)
	rec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec2, req)
	if strings.Contains(rec2.Body.String(), `name="password"`) {
		t.Fatal("minted session cookie did not authenticate")
	}
}

func TestProxyAuthTokenWrongOrMissingRequiresLogin(t *testing.T) {
	srv, _ := newAuthTestServer(t)
	srv.Cfg.ProxyAuthToken = testProxyToken
	for _, h := range []string{"", "wrong", testProxyToken + "x"} {
		rec := getWithProxyHeader(srv, "/analytics", h)
		if !strings.Contains(rec.Body.String(), `name="password"`) {
			t.Fatalf("header %q: login form not served", h)
		}
	}
}

func TestProxyAuthTokenUnsetIgnoresHeader(t *testing.T) {
	srv, _ := newAuthTestServer(t)
	for _, h := range []string{"", "anything", testProxyToken} {
		rec := getWithProxyHeader(srv, "/analytics", h)
		if !strings.Contains(rec.Body.String(), `name="password"`) {
			t.Fatalf("token unset but header %q was honoured", h)
		}
	}
}

func TestProxyAuthTokenAuthorsPost(t *testing.T) {
	srv, _ := newAuthTestServer(t)
	srv.Cfg.ProxyAuthToken = testProxyToken
	req := httptest.NewRequest("POST", "/gate/checkin", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-SC-Proxy-Auth", testProxyToken)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), `name="password"`) {
		t.Fatal("gated POST demanded login despite a valid proxy token")
	}
}
