// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"reflect"
	"testing"
)

func sb(read, write, hosts []string, keychain bool) *ManifestSandbox {
	s := &ManifestSandbox{VendorHosts: hosts, MacOSKeychain: keychain}
	s.VendorPaths.Read, s.VendorPaths.Write = read, write
	return s
}

func TestDiffPermissions(t *testing.T) {
	type want struct {
		spawn, network, hosts, read, write []string
		keychain, files, more              bool
	}
	cases := []struct {
		name     string
		old, new Permissions
		os, ns   *ManifestSandbox
		want     want
	}{
		{name: "identical", old: Permissions{Spawn: []string{"claude"}}, new: Permissions{Spawn: []string{"claude"}},
			os: sb([]string{"~/.claude"}, nil, []string{"a.com"}, true), ns: sb([]string{"~/.claude"}, nil, []string{"a.com"}, true)},
		{name: "both nil", want: want{}},
		{name: "new spawn", old: Permissions{Spawn: []string{"claude"}}, new: Permissions{Spawn: []string{"claude", "git"}},
			want: want{spawn: []string{"git"}, more: true}},
		{name: "new network", old: Permissions{Network: []string{"a.com"}}, new: Permissions{Network: []string{"a.com", "B.com"}},
			want: want{network: []string{"b.com"}, more: true}},
		{name: "host covered by wildcard", os: sb(nil, nil, []string{"*.anthropic.com"}, false), ns: sb(nil, nil, []string{"*.anthropic.com", "x.anthropic.com"}, false)},
		{name: "wildcard not covered by host", os: sb(nil, nil, []string{"api.openai.com"}, false), ns: sb(nil, nil, []string{"*.openai.com"}, false),
			want: want{hosts: []string{"*.openai.com"}, more: true}},
		{name: "narrower path covered", os: sb([]string{"~/.claude"}, nil, nil, false), ns: sb([]string{"~/.claude/projects"}, nil, nil, false)},
		{name: "broader path added", os: sb([]string{"~/.config/foo"}, nil, nil, false), ns: sb([]string{"~/.config"}, nil, nil, false),
			want: want{read: []string{"~/.config"}, more: true}},
		{name: "write compared separately", os: sb([]string{"~/.claude"}, nil, nil, false), ns: sb([]string{"~/.claude"}, []string{"~/.claude"}, nil, false),
			want: want{write: []string{"~/.claude"}, more: true}},
		{name: "keychain added", os: sb(nil, nil, nil, false), ns: sb(nil, nil, nil, true), want: want{keychain: true, more: true}},
		{name: "keychain removed", os: sb(nil, nil, nil, true), ns: sb(nil, nil, nil, false)},
		{name: "nil old sandbox", ns: sb([]string{"~/.x"}, nil, []string{"h.com"}, true),
			want: want{hosts: []string{"h.com"}, read: []string{"~/.x"}, keychain: true, more: true}},
		{name: "nil new sandbox adds nothing", os: sb([]string{"~/.x"}, nil, []string{"h.com"}, true)},
		{name: "files default equals session-cwd", old: Permissions{Files: ""}, new: Permissions{Files: "session-cwd"}},
		{name: "files changed", old: Permissions{}, new: Permissions{Files: "everywhere"}, want: want{files: true, more: true}},
		{name: "duplicates and spaces", new: Permissions{Spawn: []string{" git ", "git"}}, want: want{spawn: []string{"git"}, more: true}},
		{name: "invalid path compared raw", os: sb([]string{"foo"}, nil, nil, false), ns: sb([]string{"foo", "bar"}, nil, nil, false),
			want: want{read: []string{"bar"}, more: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := DiffPermissions(c.old, c.new, c.os, c.ns)
			got := want{d.AddedSpawn, d.AddedNetwork, d.AddedVendorHosts, d.AddedVendorPaths.Read, d.AddedVendorPaths.Write,
				d.KeychainAdded, d.FilesChanged, d.AsksForMore()}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestDiffFilesDetail(t *testing.T) {
	d := DiffPermissions(Permissions{}, Permissions{Files: "all"}, nil, nil)
	if d.OldFiles != "session-cwd" || d.NewFiles != "all" {
		t.Fatalf("got %+v", d)
	}
}
