package siteadmin

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// WriteSecretFile writes data to path through a temp file in the same
// directory and a rename, mode 0600, so a reader never sees half a key-bearing
// file and a crash never leaves one. On Windows the mode bits mean little:
// the file takes its folder's ACL, which is why every key-bearing output is
// meant for C:\ProgramData\SiteControl\ (Administrators, SYSTEM and the
// service account only — the DUS IT request, item 5) or an IT-only folder.
func WriteSecretFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			f.Close()
			os.Remove(tmp)
		}
	}()
	if err := f.Chmod(0o600); err != nil && !isWindows() {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	ok = true
	return nil
}

// BackupThenWrite keeps the current file as <path>.bak-<stamp> (same
// mode, never overwritten) and then replaces it with data. It returns the
// backup's path. A config edit is always reversible by copying it back.
func BackupThenWrite(path string, data []byte, now time.Time) (string, error) {
	old, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	bak := path + ".bak-" + now.Format("20060102-150405")
	for i := 2; fileExists(bak); i++ {
		bak = path + ".bak-" + now.Format("20060102-150405") + "-" + strconv.Itoa(i)
	}
	if err := WriteSecretFile(bak, old); err != nil {
		return "", fmt.Errorf("backup %s: %w", bak, err)
	}
	if err := WriteSecretFile(path, data); err != nil {
		return bak, err
	}
	return bak, nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func isWindows() bool { return os.PathSeparator == '\\' }

// dirEmptyOrAbsent reports whether dir can receive a fresh output set
// without mixing it with an older one.
func dirEmptyOrAbsent(dir string) (bool, error) {
	ents, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return len(ents) == 0, nil
}

// safeFileID refuses a site id that could not be a plain file name.
func safeFileID(id string) bool {
	return id != "" && !strings.ContainsAny(id, `/\:*?"<>| `) && id != "." && id != ".."
}
