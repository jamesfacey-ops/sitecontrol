// ids.go — site-prefixed reference-data identity (design §7: "every primary
// key is site-prefixed or UUIDv7 and is identity only — never ordering"; §8.3
// pins the "never ordering" half). Site-prefixing is what makes 8.7's
// duplicate-reference-data coexistence work: two sites minting a "Kakivik"
// company at the same time can never collide on ID even though the name
// matches, because no site ever mints a key in another site's namespace.
package domain

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// newID mints a site-prefixed, collision-safe identity for a new reference-data
// row: "<site_id>-<kind>-<16 hex chars of crypto/rand>". Plain crypto/rand
// rather than github.com/google/uuid: uuid is already an indirect dependency
// (pulled in transitively) but promoting it to direct is unnecessary churn on
// go.mod/go.sum in a shared checkout other chunks are also touching; 64 bits
// of randomness is more than sufficient collision resistance for this load
// (single digits of new reference rows per gate transaction, per §4).
func newID(siteID, kind string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("domain: newID: crypto/rand: %v", err))
	}
	return fmt.Sprintf("%s-%s-%s", siteID, kind, hex.EncodeToString(b[:]))
}
