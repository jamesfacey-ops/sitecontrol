package config

import (
	"fmt"
	"strings"
	"testing"
)

func proxyBody(listen, tokenLine string) string {
	return fmt.Sprintf(`
site_id               = "site-x"
listen_addr           = %q
node_epoch            = 1
db_path               = "test.db"
sync_interval_seconds = 10
login_password        = "x"
orientation           = false
site_type             = "text_message"
destinations          = []
%s
`, listen, tokenLine)
}

func TestProxyAuthTokenRefusedOnNonLoopbackListen(t *testing.T) {
	for _, addr := range []string{":8080", "0.0.0.0:8080", "10.0.0.5:8080", "[::]:8080"} {
		_, err := loadD77(t, proxyBody(addr, `proxy_auth_token = "t"`))
		if err == nil || !strings.Contains(err.Error(), "proxy_auth_token") {
			t.Fatalf("listen %q + token: err=%v, want proxy_auth_token refusal", addr, err)
		}
	}
}

func TestProxyAuthTokenLoadsOnLoopbackAndDefaultsOff(t *testing.T) {
	c, err := loadD77(t, proxyBody("127.0.0.1:8081", `proxy_auth_token = "t"`))
	if err != nil || c.ProxyAuthToken != "t" {
		t.Fatalf("loopback + token: c=%+v err=%v", c, err)
	}
	c, err = loadD77(t, proxyBody(":8081", ""))
	if err != nil || c.ProxyAuthToken != "" {
		t.Fatalf("non-loopback without token must load: c=%+v err=%v", c, err)
	}
}

func TestListenIsLoopback(t *testing.T) {
	for addr, want := range map[string]bool{"127.0.0.1:1": true, "[::1]:1": true, "localhost:1": true, ":1": false, "0.0.0.0:1": false, "junk": false} {
		if got := listenIsLoopback(addr); got != want {
			t.Errorf("%q: got %v want %v", addr, got, want)
		}
	}
}
