package config

import (
	"strings"
	"testing"
)

// theme_cookie_domain (web/theme.go): optional, config-only, and refused at
// load when a browser could not honour it — a value that would be silently
// dropped must not start up looking configured.

func TestThemeCookieDomainDefaultsToHostOnly(t *testing.T) {
	c, err := loadD77(t, proxyBody("127.0.0.1:8081", ""))
	if err != nil {
		t.Fatal(err)
	}
	if c.ThemeCookieDomain != "" || c.ThemeCookieDomainName() != "" {
		t.Fatalf("unset theme_cookie_domain: got %q / %q, want both empty (host-only cookie)", c.ThemeCookieDomain, c.ThemeCookieDomainName())
	}
}

func TestThemeCookieDomainLoadsAPlainDomain(t *testing.T) {
	for in, want := range map[string]string{".jnkfacey.com": "jnkfacey.com", "jnkfacey.com": "jnkfacey.com", ".sc.example-site.org": "sc.example-site.org"} {
		c, err := loadD77(t, proxyBody("127.0.0.1:8081", `theme_cookie_domain = "`+in+`"`))
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if c.ThemeCookieDomainName() != want {
			t.Errorf("%q: ThemeCookieDomainName = %q, want %q", in, c.ThemeCookieDomainName(), want)
		}
	}
}

func TestThemeCookieDomainRefusesWhatABrowserWouldDrop(t *testing.T) {
	for _, bad := range []string{
		".com", "com", ".", "..jnkfacey.com", "jnkfacey..com", "https://jnkfacey.com",
		"jnkfacey.com:443", "jnkfacey.com/", "JNKFACEY.COM", "*.jnkfacey.com", "jnk facey.com",
		"10.0.0.150", ".10.0.0.150", "-bad.com", "bad-.com", "jnkfacey.com.",
		"a." + strings.Repeat("b", 64) + ".com",
	} {
		_, err := loadD77(t, proxyBody("127.0.0.1:8081", `theme_cookie_domain = "`+bad+`"`))
		if err == nil || !strings.Contains(err.Error(), "theme_cookie_domain") {
			t.Errorf("%q: err=%v, want a theme_cookie_domain refusal", bad, err)
		}
	}
}
