// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"slices"
	"sort"
	"strings"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

// PermissionDiff lists what an updated manifest asks for beyond the old one.
// Removals are not reported: the host only warns about increases.
type PermissionDiff struct {
	AddedSpawn       []string
	AddedNetwork     []string
	AddedVendorHosts []string
	AddedVendorPaths protocol.VendorPaths
	KeychainAdded    bool
	FilesChanged     bool
	OldFiles         string
	NewFiles         string
}

// AsksForMore reports whether the update needs the user's attention.
func (d PermissionDiff) AsksForMore() bool {
	return len(d.AddedSpawn) > 0 || len(d.AddedNetwork) > 0 || len(d.AddedVendorHosts) > 0 ||
		len(d.AddedVendorPaths.Read) > 0 || len(d.AddedVendorPaths.Write) > 0 ||
		d.KeychainAdded || d.FilesChanged
}

// DiffPermissions compares the permissions and sandbox blocks of two versions
// of a manifest. A nil sandbox means nothing was allowed.
func DiffPermissions(old, new Permissions, oldSandbox, newSandbox *ManifestSandbox) PermissionDiff {
	var d PermissionDiff
	d.AddedSpawn = added(old.Spawn, new.Spawn, strings.TrimSpace, func(have []string, x string) bool { return slices.Contains(have, x) })
	d.AddedNetwork = added(old.Network, new.Network, normHost, hostCovered)

	if oldSandbox == nil {
		oldSandbox = &ManifestSandbox{}
	}
	if newSandbox == nil {
		newSandbox = &ManifestSandbox{}
	}
	d.AddedVendorHosts = added(oldSandbox.VendorHosts, newSandbox.VendorHosts, normHost, hostCovered)
	d.AddedVendorPaths.Read = added(oldSandbox.VendorPaths.Read, newSandbox.VendorPaths.Read, normPath, pathCovered)
	d.AddedVendorPaths.Write = added(oldSandbox.VendorPaths.Write, newSandbox.VendorPaths.Write, normPath, pathCovered)
	d.KeychainAdded = newSandbox.MacOSKeychain && !oldSandbox.MacOSKeychain

	of, nf := filesScope(old.Files), filesScope(new.Files)
	if of != nf {
		d.FilesChanged, d.OldFiles, d.NewFiles = true, of, nf
	}
	return d
}

func filesScope(s string) string {
	if s == "" {
		return "session-cwd"
	}
	return s
}

// added returns the sorted, de-duplicated entries of next that no entry of
// prev covers. Entries are normalised with norm; covered decides coverage.
func added(prev, next []string, norm func(string) string, covered func([]string, string) bool) []string {
	have := make([]string, 0, len(prev))
	for _, p := range prev {
		have = append(have, norm(p))
	}
	var out []string
	for _, n := range next {
		n = norm(n)
		if n == "" || covered(have, n) || slices.Contains(out, n) {
			continue
		}
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func normHost(h string) string { return strings.ToLower(strings.TrimSpace(h)) }

// hostCovered: an identical entry, or an old "*.suffix" wildcard that the new
// host falls under. A new wildcard is covered only by the same wildcard.
func hostCovered(have []string, h string) bool {
	if slices.Contains(have, h) {
		return true
	}
	if strings.HasPrefix(h, "*.") {
		return false
	}
	for _, o := range have {
		if suffix, ok := strings.CutPrefix(o, "*"); ok && strings.HasPrefix(suffix, ".") && strings.HasSuffix(h, suffix) {
			return true
		}
	}
	return false
}

func normPath(p string) string {
	if clean, code, _ := normalizeVendorPath(p); code == "" {
		return clean
	}
	return strings.TrimSpace(p)
}

// pathCovered: an old path equals the new one or is an ancestor of it.
func pathCovered(have []string, p string) bool {
	return slices.ContainsFunc(have, func(o string) bool { return pathWithin(p, o) })
}
