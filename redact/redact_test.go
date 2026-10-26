// SPDX-License-Identifier: Apache-2.0

package redact

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

const pem = "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA\nabcdefghijklmnop\n-----END RSA PRIVATE KEY-----"

func TestRedactor_Patterns(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"sk-ant", "sk-ant-api03-ABCDEFGH12345", Replacement},
		{"sk generic", "key=sk-proj-abcdefghijklmnopqrstuvwxyz0123", "key=" + Replacement},
		{"bearer", "Authorization: Bearer abcdef0123456789XYZ", "Authorization: Bearer " + Replacement},
		{"bearer lower", "bearer abcdef0123456789xyz", "bearer " + Replacement},
		{"pem", "before\n" + pem + "\nafter", "before\n" + Replacement + "\nafter"},
		{"pem unfinished", "x -----BEGIN OPENSSH PRIVATE KEY-----\nabc\ndef", "x " + Replacement},
		{"pgp", "-----BEGIN PGP PRIVATE KEY BLOCK-----\nabc\n-----END PGP PRIVATE KEY BLOCK-----", Replacement},
		{"two pems", pem + " and " + pem, Replacement + " and " + Replacement},
	}
	for _, rd := range []*Redactor{New(), nil} {
		for _, c := range cases {
			if got := rd.String(c.in); got != c.want {
				t.Errorf("%s (nil=%v): got %q, want %q", c.name, rd == nil, got, c.want)
			}
		}
	}
}

func TestRedactor_NoFalsePositives(t *testing.T) {
	for _, in := range []string{
		"task-management-something-very-long-name",
		"sk-short",
		"sk-ant-short",
		"Bearer authentication",
		"the bearer of bad news",
		"An ordinary paragraph, with punctuation: nothing secret here.",
	} {
		if got := New().String(in); got != in {
			t.Errorf("changed %q to %q", in, got)
		}
	}
}

func TestRedactor_Add(t *testing.T) {
	r := New()
	r.Add("short")
	if got := r.String("a short b"); got != "a short b" {
		t.Fatalf("short secret was registered: %q", got)
	}
	r.Add("12345678")
	r.Add("12345678") // duplicate
	if got := r.String("x12345678y 12345678"); got != "x"+Replacement+"y "+Replacement {
		t.Fatalf("got %q", got)
	}
	r.Add("пароль123") // 9 runes, 17 bytes
	if got := r.String("p=пароль123"); got != "p="+Replacement {
		t.Fatalf("multi-byte secret: %q", got)
	}
	r.Add("пароль1") // 7 runes: ignored
	if got := r.String("пароль1"); got != "пароль1" {
		t.Fatalf("7-rune secret was registered: %q", got)
	}
}

func TestRedactor_LongestFirst(t *testing.T) {
	for _, order := range [][2]string{{"abcdefgh", "abcdefghijkl"}, {"abcdefghijkl", "abcdefgh"}} {
		r := New()
		r.Add(order[0])
		r.Add(order[1])
		if got := r.String("abcdefghijkl"); got != Replacement {
			t.Errorf("order %v: got %q", order, got)
		}
	}
}

func TestRedactor_Idempotent(t *testing.T) {
	r := New()
	r.Add("topsecret-value")
	for _, in := range []string{"sk-ant-api03-ABCDEFGH12345", "Bearer abcdef0123456789XYZ", pem, "x topsecret-value y", "plain"} {
		once := r.String(in)
		if twice := r.String(once); twice != once {
			t.Errorf("not idempotent for %q: %q then %q", in, once, twice)
		}
	}
}

func TestRedactor_EmptyAndNil(t *testing.T) {
	if got := New().String(""); got != "" {
		t.Fatalf("got %q", got)
	}
	var r *Redactor
	if got := r.String("sk-ant-api03-ABCDEFGH"); got != Replacement {
		t.Fatalf("nil receiver: %q", got)
	}
}

func TestRedactor_Concurrent(t *testing.T) {
	r := New()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			r.Add(fmt.Sprintf("concurrent-secret-%d", i))
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = r.String("concurrent-secret-3 and more")
			}
		}()
	}
	wg.Wait()
	if got := r.String("concurrent-secret-3"); !strings.Contains(got, Replacement) || strings.Contains(got, "secret-3") {
		t.Fatalf("got %q", got)
	}
}
