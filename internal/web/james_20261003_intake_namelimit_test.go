package web

import (
	"net/http"
	"strings"
	"testing"
)

// James, 2026-10-03: the /roster enrolment (intake) form gets the same
// 120-character name limit W3 added at the gate, in the same words,
// enforced server-side (the form has no script, so JS on and off are the
// same request).

func TestIntakeRefusesAnOverlongNameWithTheGatesWordingAndMintsNothing(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	companyID := companyIDNamed(t, srv, "Kakivik Asset Management")
	p0, c0, w0 := countRows(t, srv)
	last := strings.Repeat("x", 128) // stored "xxx…, Mary" = 134 characters
	rec := postForm(t, srv, "/roster/intake", intakeForm2(last, "Mary", companyID, "", "", ""))
	body := rec.Body.String()
	if rec.Code != http.StatusConflict {
		t.Fatalf("a 134-character name = %d, want 409", rec.Code)
	}
	want := "The name typed is 134 characters long — longer than any real name (the limit is 120), so it looks like something was pasted or typed by accident. Nothing has been recorded and the form below is intact; check the name and shorten it."
	if !strings.Contains(body, want) {
		t.Fatalf("intake refusal does not carry the gate's wording %q:\n%s", want, body)
	}
	if gate := overlongNameSentence(134, "the list below"); !strings.Contains(want, strings.TrimPrefix(gate, "the name typed")[:60]) {
		t.Fatalf("intake and gate wordings diverged: gate %q", gate)
	}
	if !strings.Contains(body, `value="`+last+`"`) {
		t.Errorf("the typed last name was not kept on the re-rendered form")
	}
	if p1, c1, w1 := countRows(t, srv); p1 != p0 || c1 != c0 || w1 != w0 {
		t.Errorf("a refused intake wrote rows: persons %d->%d companies %d->%d trades %d->%d", p0, p1, c0, c1, w0, w1)
	}
}

func TestIntakeAcceptsANameAtTheLimit(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	companyID := companyIDNamed(t, srv, "Kakivik Asset Management")
	last := strings.Repeat("y", 114) // "yyy…, Mary" = exactly 120
	rec := postForm(t, srv, "/roster/intake", intakeForm2(last, "Mary", companyID, "", "", ""))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("a 120-character name = %d, want 303 (enrolled): %s", rec.Code, rec.Body.String())
	}
	if got := personNamed(t, srv, last+", Mary"); len(got) != 1 {
		t.Fatalf("want 1 person enrolled under the 120-character name, got %d", len(got))
	}
}
