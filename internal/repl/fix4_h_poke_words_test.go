package repl

import (
	"strings"
	"testing"
)

// TestFix4HPokeLogWordsADroppedConnection — R6-2.3 (fix4 H): the POKES ARE
// FAILING log line said "nothing answered" for an EOF. FAIL BEFORE.
func TestFix4HPokeLogWordsADroppedConnection(t *testing.T) {
	eof := pokeFailureWords(ErrorKindUnreachable, `Post "http://127.0.0.1:21032/sync/poke?t1=1": EOF`)
	if strings.Contains(eof, "nothing answered") || !strings.Contains(eof, "dropped") {
		t.Errorf("an EOF poke is worded %q", eof)
	}
	refused := pokeFailureWords(ErrorKindUnreachable, `Post "http://127.0.0.1:9/sync/poke": dial tcp 127.0.0.1:9: connect: connection refused`)
	if !strings.Contains(refused, "nothing answered") {
		t.Errorf("a refused connection is worded %q", refused)
	}
}
