// SPDX-License-Identifier: Apache-2.0

// Package manifest loads and validates arvel.json, the manifest of an Arvel
// plugin (docs/PROTOCOL.md §3.2, §4, §14.1, §16).
package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

// maxManifestSize bounds what Load reads.
const maxManifestSize = 1 << 20

// PluginType is the "type" of a manifest.
type PluginType string

const (
	TypeProvider    PluginType = "provider"
	TypeRouter      PluginType = "router"
	TypeTeam        PluginType = "team"
	TypeIntegration PluginType = "integration"
)

// Valid reports whether t is a plugin type this SDK handles.
func (t PluginType) Valid() bool {
	return t == TypeProvider || t == TypeRouter || t == TypeTeam || t == TypeIntegration
}

// Declarative reports whether the plugin is data only (PROTOCOL §16): a team or
// integration plugin.
func (t PluginType) Declarative() bool { return t == TypeTeam || t == TypeIntegration }

// ManifestSandbox is the "sandbox" block (PROTOCOL §3.2, §15).
type ManifestSandbox = protocol.ManifestSandbox

// Manifest mirrors arvel.json. Unknown fields are ignored when loading.
type Manifest struct {
	Schema       string           `json:"$schema,omitempty"`
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Type         PluginType       `json:"type"`
	Version      string           `json:"version"`
	PluginAPI    string           `json:"pluginApi"`
	Description  string           `json:"description,omitempty"`
	Homepage     string           `json:"homepage,omitempty"`
	License      string           `json:"license,omitempty"`
	Entry        EntrySpec        `json:"entry,omitzero"`
	Checksums    string           `json:"checksums,omitempty"`
	Requires     []Requirement    `json:"requires,omitempty"`
	Permissions  Permissions      `json:"permissions"`
	Sandbox      *ManifestSandbox `json:"sandbox,omitempty"`
	Connections  []Connection     `json:"connections,omitempty"`
	Capabilities Capabilities     `json:"capabilities,omitzero"`
	Contributes  *Contributes     `json:"contributes,omitempty"`

	raw []byte // the document passed to Load, validated against the schema
}

// Requirement is an external program the plugin drives.
type Requirement struct {
	Binary      string            `json:"binary"`
	MinVersion  string            `json:"minVersion,omitempty"`
	VersionArgs []string          `json:"versionArgs,omitempty"`
	Install     map[string]string `json:"install,omitempty"`
}

// Permissions is what the plugin may do (PROTOCOL §3.2).
type Permissions struct {
	Spawn   []string `json:"spawn,omitempty"`
	Network []string `json:"network,omitempty"`
	Files   string   `json:"files,omitempty"`
}

// Connection is one connection kind.
type Connection struct {
	Kind        string           `json:"kind"`
	Label       string           `json:"label"`
	Billing     protocol.Billing `json:"billing"`
	Description string           `json:"description,omitempty"`
	Config      ConfigSchema     `json:"config,omitzero"`
	Secrets     []Secret         `json:"secrets,omitempty"`
}

// Secret is a value the host keeps in the keychain and passes in initialize.
type Secret struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Pattern  string `json:"pattern,omitempty"`
	Required bool   `json:"required,omitempty"`
}

// ConfigSchema is the JSON Schema subset of a connection's settings form.
type ConfigSchema struct {
	Type        string                    `json:"type,omitempty"`
	Title       string                    `json:"title,omitempty"`
	Description string                    `json:"description,omitempty"`
	Properties  map[string]ConfigProperty `json:"properties,omitempty"`
}

// ConfigProperty is one form field: boolean, string or integer.
type ConfigProperty struct {
	Type        string   `json:"type"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Default     any      `json:"default,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Pattern     string   `json:"pattern,omitempty"`
	Minimum     *int64   `json:"minimum,omitempty"`
	Maximum     *int64   `json:"maximum,omitempty"`
}

// Capabilities are static hints for the UI.
type Capabilities struct {
	Usage        string              `json:"usage,omitempty"`
	Subagents    bool                `json:"subagents,omitempty"`
	Resume       bool                `json:"resume,omitempty"`
	ModelSwitch  string              `json:"modelSwitch,omitempty"`
	EffortSwitch string              `json:"effortSwitch,omitempty"`
	Router       *RouterCapabilities `json:"router,omitempty"`
}

// RouterCapabilities are the router hints of a manifest.
type RouterCapabilities struct {
	Features     []string `json:"features,omitempty"`
	MaxLatencyMs int      `json:"maxLatencyMs,omitempty"`
}

// Entry is how to start the plugin on one platform: a release asset and the
// binary inside it, or a command (generic ACP agent).
type Entry struct {
	Asset   string   `json:"asset,omitempty"`
	Bin     string   `json:"bin,omitempty"`
	Command []string `json:"command,omitempty"`
}

// EntrySpec is the "entry" member: a map from "os-arch" to a release asset, or
// a single command entry that is valid on every platform.
type EntrySpec struct {
	Platforms map[string]Entry
	Command   []string
}

// IsCommand reports whether the entry is a command entry.
func (e EntrySpec) IsCommand() bool { return len(e.Command) > 0 }

// UnmarshalJSON accepts both shapes of "entry".
func (e *EntrySpec) UnmarshalJSON(data []byte) error {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil {
		return fmt.Errorf("entry: %w", err)
	}
	*e = EntrySpec{}
	if rawCmd, ok := members["command"]; ok {
		if len(members) > 1 {
			return errors.New("entry: command cannot be combined with platform keys")
		}
		return json.Unmarshal(rawCmd, &e.Command)
	}
	e.Platforms = make(map[string]Entry, len(members))
	for k, v := range members {
		var ent Entry
		if err := json.Unmarshal(v, &ent); err != nil {
			return fmt.Errorf("entry %q: %w", k, err)
		}
		e.Platforms[k] = ent
	}
	return nil
}

// MarshalJSON writes the shape that was loaded.
func (e EntrySpec) MarshalJSON() ([]byte, error) {
	if e.IsCommand() {
		return json.Marshal(map[string][]string{"command": e.Command})
	}
	if e.Platforms == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(e.Platforms)
}

// Load decodes a manifest from r. It does not validate it; call Validate.
func Load(r io.Reader) (*Manifest, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxManifestSize+1))
	if err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	if len(data) > maxManifestSize {
		return nil, errors.New("manifest: larger than 1 MiB")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		var ute *json.UnmarshalTypeError
		if errors.As(err, &ute) {
			return nil, fmt.Errorf("manifest: field %q: %w", ute.Field, err)
		}
		return nil, fmt.Errorf("manifest: %w", err)
	}
	if dec.More() {
		return nil, errors.New("manifest: unexpected data after the JSON document")
	}
	m.raw = data
	return &m, nil
}

// LoadFile reads a manifest from a file.
func LoadFile(path string) (*Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("manifest %s: %w", path, err)
	}
	defer f.Close()
	m, err := Load(f)
	if err != nil {
		return nil, fmt.Errorf("manifest %s: %w", path, err)
	}
	return m, nil
}

// Platforms returns the six os-arch keys every official plugin supports.
func Platforms() []string {
	return []string{"darwin-arm64", "darwin-amd64", "linux-amd64", "linux-arm64", "windows-amd64", "windows-arm64"}
}

// EntryFor returns the entry to use on goos/goarch. A command entry is valid
// on every platform.
func (m *Manifest) EntryFor(goos, goarch string) (Entry, bool) {
	if m.Entry.IsCommand() {
		return Entry{Command: slices.Clone(m.Entry.Command)}, true
	}
	ent, ok := m.Entry.Platforms[goos+"-"+goarch]
	if !ok {
		return Entry{}, false
	}
	return ent, true
}
