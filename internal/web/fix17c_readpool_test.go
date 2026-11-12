package web

// FIX17c: a page never waits for a write's commit. A check-in whose commit
// is stuck — writes complete, not yet visible, the writer held, which is
// where a commit's fsync stall sits — leaves /muster, /gate and /analytics
// serving the state before it; once the commit returns, the very next page
// shows it, and so does the page the POST redirects to.

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func fix17cGet(root http.Handler, cookie *http.Cookie, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, req)
	return rec
}

func fix17cPost(root http.Handler, cookie *http.Cookie, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, req)
	return rec
}

func fix17cSingleCheckIn(t *testing.T, srv *Server, name string) url.Values {
	t.Helper()
	vals := singleCheckInForm(t, srv, name)
	vals.Set("new_company_id", newRef)
	vals.Set("new_company", "FIX17c Co")
	return vals
}

func TestFix17cPagesServeTheStateBeforeAStuckCommit(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	root := srv.Root()
	cookie := loginCookie(t, srv, testLoginPassword)
	pages := []string{"/muster", "/gate", "/analytics"}
	for _, p := range pages { // warm templates and memo
		if rec := fix17cGet(root, cookie, p); rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", p, rec.Code)
		}
	}
	const name = "Stuck Commitson"
	vals := fix17cSingleCheckIn(t, srv, name)

	entered, out := make(chan struct{}), make(chan struct{})
	var once, rel sync.Once
	release := func() { rel.Do(func() { close(out) }) }
	t.Cleanup(release)
	st.BeforeCommit = func() { once.Do(func() { close(entered); <-out }) }

	posted := make(chan *httptest.ResponseRecorder, 1)
	go func() { posted <- fix17cPost(root, cookie, "/gate/checkin/single", vals) }()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the check-in's commit never reached the stall")
	}

	// Every page, several officers at once, while the commit is stuck.
	const bound = 3 * time.Second
	type result struct {
		path string
		code int
		body string
		took time.Duration
	}
	results := make(chan result, 3*len(pages))
	for i := 0; i < 3; i++ {
		for _, p := range pages {
			go func(p string) {
				t0 := time.Now()
				rec := fix17cGet(root, cookie, p)
				results <- result{p, rec.Code, rec.Body.String(), time.Since(t0)}
			}(p)
		}
	}
	deadline := time.After(bound)
	for i := 0; i < 3*len(pages); i++ {
		select {
		case r := <-results:
			if r.code != http.StatusOK {
				t.Errorf("GET %s during the stuck commit = %d", r.path, r.code)
			}
			if r.path == "/muster" && strings.Contains(r.body, name) {
				t.Errorf("FIX17c: /muster shows %s while the commit that records them is still in flight", name)
			}
			t.Logf("GET %s during the stuck commit: %v", r.path, r.took.Round(time.Millisecond))
		case <-deadline:
			release()
			t.Fatalf("FIX17c: %d of %d page loads had not returned %s into a stuck commit — pages are waiting for the writer", 3*len(pages)-i, 3*len(pages), bound)
		}
	}

	release()
	rec := <-posted
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("check-in = %d: %s", rec.Code, rec.Body.String())
	}
	if body := fix17cGet(root, cookie, "/muster").Body.String(); !strings.Contains(body, name) {
		t.Fatalf("FIX17c: once the commit returned, /muster still does not show %s", name)
	}
}

// TestFix17cThePageAfterAPostShowsIt is read-your-writes across POST ->
// redirect -> GET, with other officers loading pages the whole time (so the
// memo and the pool's connections are warm with the state before each
// write).
func TestFix17cThePageAfterAPostShowsIt(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	root := srv.Root()
	cookie := loginCookie(t, srv, testLoginPassword)
	var stop sync.WaitGroup
	quit := make(chan struct{})
	for i := 0; i < 3; i++ {
		stop.Add(1)
		go func(i int) {
			defer stop.Done()
			for {
				select {
				case <-quit:
					return
				default:
				}
				fix17cGet(root, cookie, []string{"/muster", "/gate", "/analytics"}[i])
			}
		}(i)
	}
	defer func() { close(quit); stop.Wait() }()

	for i := 0; i < 8; i++ {
		name := "Redirect Readerson " + string(rune('A'+i))
		rec := fix17cPost(root, cookie, "/gate/checkin/single", fix17cSingleCheckIn(t, srv, name))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("check-in %d = %d: %s", i, rec.Code, rec.Body.String())
		}
		loc := rec.Header().Get("Location")
		if !strings.HasPrefix(loc, "/gate") {
			t.Fatalf("check-in redirected to %q, want /gate", loc)
		}
		if page := fix17cGet(root, cookie, loc); page.Code != http.StatusOK {
			t.Fatalf("GET %s after the check-in = %d", loc, page.Code)
		}
		if body := fix17cGet(root, cookie, "/muster").Body.String(); !strings.Contains(body, name) {
			t.Fatalf("FIX17c: the muster loaded right after %s's check-in does not show them", name)
		}
	}
}
