package web

// James, 2026-10-03: duplicate-submit lock covers deliberate declines
// (declinelock.go). N identical "No — this is a different person" check-ins
// sent at the same moment mint ONE namesake with ONE open visit and all land
// on the same confirmation; the same decline outside the window, or with a
// different company, is a genuinely new decline and mints. Red before: the
// concurrent half minted N namesakes, each checked in (muster N, not 1).

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// declineBody is the body the script posts after "No — this is a different
// person" for chris's name with the given company picked.
func declineBody(t *testing.T, srv *Server, name, companyID string) url.Values {
	t.Helper()
	vals := singleCheckInForm(t, srv, name)
	vals.Set("new_kind", "new")
	vals.Set("new_value", name)
	vals.Set("new_company_id", companyID)
	return vals
}

// namesakes: persons called name other than the original, and how many of
// them hold an open visit at site-a.
func namesakes(t *testing.T, st *store.Store, name, originalID string) (persons, openVisits int) {
	t.Helper()
	all, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	minted := map[string]bool{}
	for _, p := range all {
		if p.PersonID != originalID && strings.EqualFold(p.Name, name) {
			minted[p.PersonID] = true
		}
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range open {
		if minted[v.PersonID] {
			openVisits++
		}
	}
	return len(minted), openVisits
}

func TestIdenticalConcurrentDeclinesMintOneNamesakeWithOneVisit(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	chris := persons[2]
	aurora, err := domain.CreateCompany(st, "site-a", 1, "Aurora Mechanical", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	body := declineBody(t, srv, chris.Name, aurora.CompanyID).Encode()
	cookie := loginCookie(t, srv, testLoginPassword)
	h := srv.Handler()

	const n = 6
	var wg sync.WaitGroup
	codes := make([]int, n)
	locs := make([]string, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest("POST", "/gate/checkin/single", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			<-start
			h.ServeHTTP(rec, req)
			codes[i], locs[i] = rec.Code, rec.Header().Get("Location")
		}(i)
	}
	close(start)
	wg.Wait()
	for i := range codes {
		if codes[i] != http.StatusSeeOther {
			t.Fatalf("submission %d = %d, want 303", i, codes[i])
		}
		if locs[i] != locs[0] {
			t.Errorf("submission %d landed on %q, the first on %q — want the same confirmation", i, locs[i], locs[0])
		}
	}
	if p, v := namesakes(t, st, chris.Name, chris.PersonID); p != 1 || v != 1 {
		t.Fatalf("%d identical declines minted %d namesake(s) with %d open visit(s), want 1 and 1", n, p, v)
	}
}

func TestADeclineOutsideTheWindowOrWithAnotherCompanyStillMints(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	chris := persons[2]
	now := time.Now()
	srv.SubmitNow = func() time.Time { return now }
	aurora, err := domain.CreateCompany(st, "site-a", 1, "Aurora Mechanical", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	boreal, err := domain.CreateCompany(st, "site-a", 1, "Boreal Welding", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	post := func(company string) {
		t.Helper()
		if rec := postForm(t, srv, "/gate/checkin/single", declineBody(t, srv, chris.Name, company)); rec.Code != http.StatusSeeOther {
			t.Fatalf("decline = %d: %s", rec.Code, rec.Body.String())
		}
	}
	post(aurora.CompanyID)
	post(aurora.CompanyID) // inside the window: the same outcome, no mint
	if p, _ := namesakes(t, st, chris.Name, chris.PersonID); p != 1 {
		t.Fatalf("after a resubmit inside the window: %d namesakes, want 1", p)
	}
	post(boreal.CompanyID) // a different company: a genuinely new decline
	if p, _ := namesakes(t, st, chris.Name, chris.PersonID); p != 2 {
		t.Fatalf("a decline with another company: %d namesakes, want 2", p)
	}
	now = now.Add(duplicateSubmitWindow + time.Second)
	post(aurora.CompanyID) // outside the window: a new decline mints
	if p, _ := namesakes(t, st, chris.Name, chris.PersonID); p != 3 {
		t.Fatalf("the same decline outside the window: %d namesakes, want 3", p)
	}
}
