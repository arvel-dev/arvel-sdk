// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"
)

// InitMeta is "_meta.arvel" of the initialize request (PROTOCOL §5.1).
type InitMeta struct {
	PluginAPI  string         `json:"pluginApi"`
	Connection ConnectionInfo `json:"connection"`
	DataDir    string         `json:"dataDir"`
	Locale     string         `json:"locale,omitempty"`
	Platform   PlatformInfo   `json:"platform"`
	Sandbox    *SandboxInfo   `json:"sandbox,omitempty"`
}

// ConnectionInfo describes the connection instance a process serves. Secrets
// are delivered here and nowhere else; String, GoString and LogValue print the
// secret names only, never the values.
type ConnectionInfo struct {
	ID      string            `json:"id"`
	Kind    string            `json:"kind"`
	Config  map[string]any    `json:"config,omitempty"`
	Secrets map[string]string `json:"secrets,omitempty"`
}

func (c ConnectionInfo) secretNames() []string {
	names := make([]string, 0, len(c.Secrets))
	for k := range c.Secrets {
		names = append(names, k)
	}
	slices.Sort(names)
	return names
}

// String prints the connection without secret values.
func (c ConnectionInfo) String() string {
	return fmt.Sprintf("ConnectionInfo{id=%s kind=%s secrets=[%s]}", c.ID, c.Kind, strings.Join(c.secretNames(), " "))
}

// GoString keeps %#v from leaking secrets.
func (c ConnectionInfo) GoString() string { return c.String() }

// LogValue implements slog.LogValuer without secret values.
func (c ConnectionInfo) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("id", c.ID),
		slog.String("kind", c.Kind),
		slog.Any("secrets", c.secretNames()),
	)
}

// PlatformInfo describes the machine the host runs on.
type PlatformInfo struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	Remote bool   `json:"remote"`
}

// InitResultMeta is "_meta.arvel" of the initialize response.
type InitResultMeta struct {
	PluginAPI        string      `json:"pluginApi"`
	Features         []string    `json:"features"`
	InstructionFiles []string    `json:"instructionFiles,omitempty"`
	Skills           *SkillsInfo `json:"skills,omitempty"`
}

// SkillsInfo tells where the vendor CLI looks for project skills.
type SkillsInfo struct {
	ProjectDirs []string `json:"projectDirs,omitempty"`
	Spec        string   `json:"spec,omitempty"`
}

// Has reports whether feature is announced.
func (r InitResultMeta) Has(feature string) bool {
	return slices.Contains(r.Features, feature)
}
