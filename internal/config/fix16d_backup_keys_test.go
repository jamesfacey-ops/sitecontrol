package config

// FIX16 D: backup_dir / backup_mirror_dir / backup_time are OPTIONAL. Every
// shipped toml (none carries them) still loads, with the defaults; a file
// that carries them loads with its values; a malformed time is refused by
// name.

import (
	"testing"
)

// FIX17b (sc-stress F19): posts with no backup_time are spread over 03:00-03:14
// by site_id, the same on every start; an explicit backup_time is exact.
func TestFix17bDefaultBackupTimeIsOffsetPerSite(t *testing.T) {
	seen := map[int]string{}
	for _, id := range []string{"ice-road", "ice-pad", "npf", "ndb", "sid"} {
		c := &Config{SiteID: id}
		h, m := c.BackupAt()
		if h != 3 || m != BackupOffsetMinutes(id) || m > 14 {
			t.Fatalf("%s: default backup %02d:%02d", id, h, m)
		}
		if h2, m2 := (&Config{SiteID: id}).BackupAt(); h2 != h || m2 != m {
			t.Fatalf("%s: the offset is not deterministic", id)
		}
		if other, dup := seen[m]; dup {
			t.Fatalf("%s and %s back up in the same minute", id, other)
		}
		seen[m] = id
		c.BackupTime = "03:00"
		if h, m := c.BackupAt(); h != 3 || m != 0 {
			t.Fatalf("%s: an explicit backup_time 03:00 ran at %02d:%02d", id, h, m)
		}
	}
}
