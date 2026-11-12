// Package siteadmin is `sitectl admin` (FIX16 H, design
// fork-docs/SITE-TOOL-DESIGN.md, lead-approved 2026-10-06): the IT tool that
// generates a season's per-post configs, adds a post to a running mesh,
// swaps a post's laptop, and retires a post. It never runs inside a serving
// post and authors nothing into any log; its only writes are config files
// (and, through the restore hook, a database a STOPPED post is about to
// start from).
//
// THE KEY RULE. Every post has one peer_key — what it accepts — and every
// other post holds that key in its [[peers]] entry (repl/server.go
// requireKey). So any one post's site.toml already holds every key in the
// mesh, and no separate keyring is ever kept. Keys and login passwords are
// never printed, never put in argv and never logged: output names a key only
// by its Fingerprint.
package siteadmin

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// NewKey returns a fresh peer key: 32 random bytes, base64url without
// padding (43 characters, safe inside a TOML basic string and an HTTP
// Authorization header).
func NewKey() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("siteadmin: crypto/rand failed: " + err.Error()) // never happens on a working OS; never degrade to a weak key
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// passwordAlphabet has no 0/O, 1/l/I: a login password is read off a
// handover sheet and typed by an officer.
const passwordAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// NewPassword returns a fresh login password: 16 characters from
// passwordAlphabet in four groups of four ("k7m2-p9qx-…", about 79 bits).
func NewPassword() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("siteadmin: crypto/rand failed: " + err.Error())
	}
	out := make([]byte, 0, 19)
	for i, c := range b {
		if i > 0 && i%4 == 0 {
			out = append(out, '-')
		}
		// 256 % 31 != 0, so this is very slightly biased toward the first
		// 8 letters; at 16 characters that costs well under one bit.
		out = append(out, passwordAlphabet[int(c)%len(passwordAlphabet)])
	}
	return string(out)
}

// Fingerprint names a secret without revealing it: "fp:" and the first 8
// hex digits of its SHA-256. Two posts' files can be compared by eye (does
// NPF hold the key Ice Pad accepts?) and nothing about the key leaks.
func Fingerprint(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return "fp:" + hex.EncodeToString(h[:4])
}
