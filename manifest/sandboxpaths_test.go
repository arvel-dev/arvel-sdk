// SPDX-License-Identifier: Apache-2.0

package manifest

import "testing"

func TestCheckVendorPath(t *testing.T) {
	accepted := []string{
		"~/.claude", "~/.claude.json", "~/.config", "~/.config/claude", "~/.codex/", "/opt/vendor/state", "/home/me/.sshfs", "/home/me/.config/gcloudish",
		"C:/Users/x/.claude", `C:\Users\x\.claude`, "~/Library/Application Support/Claude",
	}
	for _, p := range accepted {
		if code, msg := checkVendorPath(p); code != "" {
			t.Errorf("%q rejected: %s %s", p, code, msg)
		}
	}
	rejected := map[string]string{
		"":                         "sandbox-path-form",
		"~":                        "sandbox-path-root",
		"/":                        "sandbox-path-root",
		"~/":                       "sandbox-path-root",
		"~/.ssh":                   "sandbox-path-forbidden",
		"~/.SSH/id_rsa":            "sandbox-path-forbidden",
		"~/.config/gcloud":         "sandbox-path-forbidden",
		"~/.config/gcloud/x":       "sandbox-path-forbidden",
		"~/.docker/config.json":    "sandbox-path-forbidden",
		`~\.aws\credentials`:       "sandbox-path-forbidden",
		"~/Library/Safari":         "sandbox-path-forbidden",
		"~/.mozilla/firefox":       "sandbox-path-forbidden",
		"~/foo/../.ssh":            "sandbox-path-dotdot",
		"~/..":                     "sandbox-path-dotdot",
		"foo":                      "sandbox-path-relative",
		"./x":                      "sandbox-path-relative",
		"~bob/x":                   "sandbox-path-form",
		"~/.kube":                  "sandbox-path-forbidden",
		"~/.gnupg/private-keys-v1": "sandbox-path-forbidden",
		// absolute paths cannot dodge the list by naming the home directory
		"/root/.ssh":                    "sandbox-path-forbidden",
		"/home/ubuntu/.aws/credentials": "sandbox-path-forbidden",
		"/Users/a/.config/gcloud":       "sandbox-path-forbidden",
		`C:\Users\a\.docker`:            "sandbox-path-forbidden",
		"/Users/a/Library/Safari/x":     "sandbox-path-forbidden",
	}
	for p, want := range rejected {
		if code, _ := checkVendorPath(p); code != want {
			t.Errorf("%q: code %q, want %q", p, code, want)
		}
	}
}

func TestAncestorOfForbiddenIsAllowed(t *testing.T) {
	// PROTOCOL §15.2: the always-denied list wins under an allowed parent.
	if code, _ := checkVendorPath("~/.config"); code != "" {
		t.Fatalf("~/.config should be allowed, got %s", code)
	}
}

func TestPathWithin(t *testing.T) {
	cases := []struct {
		p, root string
		want    bool
	}{
		{"~/.ssh", "~/.ssh", true},
		{"~/.ssh/x", "~/.ssh", true},
		{"~/.sshx", "~/.ssh", false},
		{"~", "~/.ssh", false},
		{"~/.SSH", "~/.ssh", true},
	}
	for _, c := range cases {
		if got := pathWithin(c.p, c.root); got != c.want {
			t.Errorf("pathWithin(%q,%q) = %v", c.p, c.root, got)
		}
	}
}
