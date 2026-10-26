// SPDX-License-Identifier: Apache-2.0

// Package redact scrubs secrets from text destined for logs and errors.
//
// A Redactor replaces secrets registered at run time (the values the host
// sends in initialize) and anything that looks like a well-known key format.
package redact

import (
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

// Replacement is written in place of every secret.
const Replacement = "‹redacted›"

// MinSecretLen is the shortest secret, in runes, that Add accepts.
const MinSecretLen = 8

// Redactor replaces registered secrets and well-known key formats. It is safe
// for concurrent use. A nil *Redactor is valid and applies only the patterns.
type Redactor struct {
	mu       sync.RWMutex
	secrets  []string
	replacer *strings.Replacer
}

// New returns a Redactor with no registered secrets.
func New() *Redactor { return &Redactor{} }

// Add registers a secret. Secrets shorter than MinSecretLen runes are ignored,
// because replacing short strings would corrupt ordinary text. Only the literal
// value is registered, not its JSON-escaped or URL-encoded form.
func (r *Redactor) Add(secret string) {
	if utf8.RuneCountInString(secret) < MinSecretLen {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.secrets {
		if s == secret {
			return
		}
	}
	r.secrets = append(r.secrets, secret)
	// Longest first, so a secret that contains another one is replaced whole.
	sort.SliceStable(r.secrets, func(i, j int) bool { return len(r.secrets[i]) > len(r.secrets[j]) })
	oldnew := make([]string, 0, 2*len(r.secrets))
	for _, s := range r.secrets {
		oldnew = append(oldnew, s, Replacement)
	}
	r.replacer = strings.NewReplacer(oldnew...)
}

// String returns s with every registered secret and every recognised key
// format replaced by Replacement.
func (r *Redactor) String(s string) string {
	if s == "" {
		return s
	}
	if r != nil {
		r.mu.RLock()
		rep := r.replacer
		r.mu.RUnlock()
		if rep != nil {
			s = rep.Replace(s)
		}
	}
	return applyPatterns(s)
}
