// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"path"
	"regexp"
	"strings"
)

// forbiddenVendorPaths lists the paths a manifest may never ask for, and
// everything below them (PROTOCOL §3.2, §15.2). Browser profiles are included.
func forbiddenVendorPaths() []string {
	return []string{
		"~/.ssh", "~/.gnupg", "~/.aws", "~/.kube", "~/.config/gcloud", "~/.docker",
		"~/Library/Application Support/Google/Chrome",
		"~/Library/Application Support/Firefox",
		"~/Library/Application Support/BraveSoftware",
		"~/Library/Application Support/Microsoft Edge",
		"~/Library/Safari",
		"~/.config/google-chrome", "~/.config/chromium", "~/.config/BraveSoftware", "~/.config/microsoft-edge",
		"~/.mozilla",
		"~/AppData/Local/Google/Chrome", "~/AppData/Local/Microsoft/Edge", "~/AppData/Roaming/Mozilla",
	}
}

var windowsDrive = regexp.MustCompile(`^[A-Za-z]:/`)

// normalizeVendorPath converts separators and cleans the path. It reports a
// problem code and message when the path form is not acceptable.
func normalizeVendorPath(p string) (clean, code, msg string) {
	p = strings.ReplaceAll(p, `\`, "/")
	if p == "" {
		return "", "sandbox-path-form", "path is empty"
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", "sandbox-path-dotdot", `".." is not allowed in a vendor path`
		}
	}
	switch {
	case strings.HasPrefix(p, "~") && p != "~" && !strings.HasPrefix(p, "~/"):
		return "", "sandbox-path-form", "~user paths are not supported"
	case !strings.HasPrefix(p, "~") && !strings.HasPrefix(p, "/") && !windowsDrive.MatchString(p):
		return "", "sandbox-path-relative", `path must start with "~" or be absolute`
	}
	return path.Clean(p), "", ""
}

// checkVendorPath validates one sandbox.vendorPaths entry. It returns a
// problem code and message, or empty strings when the path is acceptable.
func checkVendorPath(p string) (code, msg string) {
	clean, code, msg := normalizeVendorPath(p)
	if code != "" {
		return code, msg
	}
	if clean == "~" || clean == "/" {
		return "sandbox-path-root", "the home directory and the root cannot be granted"
	}
	for _, f := range forbiddenVendorPaths() {
		if forbiddenBy(clean, f) {
			return "sandbox-path-forbidden", clean + " is inside " + f + ", which is always denied"
		}
	}
	return "", ""
}

// forbiddenBy reports whether p is the forbidden root f or lies below it. A
// manifest cannot know the user's home directory, so an absolute path such as
// /home/me/.ssh is matched by its home-relative tail: the forbidden path
// without "~/" may start at any segment.
func forbiddenBy(p, f string) bool {
	if strings.HasPrefix(p, "~") {
		return pathWithin(p, f)
	}
	tail := strings.Split(strings.TrimPrefix(f, "~/"), "/")
	segs := strings.Split(p, "/")
	for i := range segs {
		if len(segs)-i < len(tail) {
			break
		}
		match := true
		for j, t := range tail {
			if !strings.EqualFold(segs[i+j], t) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// pathWithin reports whether p equals root or lies below it, comparing
// segments case-insensitively (macOS and Windows file systems are).
func pathWithin(p, root string) bool {
	ps, rs := strings.Split(p, "/"), strings.Split(root, "/")
	if len(ps) < len(rs) {
		return false
	}
	for i := range rs {
		if !strings.EqualFold(ps[i], rs[i]) {
			return false
		}
	}
	return true
}
