// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"errors"
	"fmt"
)

// DiscoverParams are the params of _arvel/discover (PROTOCOL §7.9).
type DiscoverParams struct {
	Kinds    []DiscoverKind `json:"kinds"`
	Projects []string       `json:"projects,omitempty"`
}

// DiscoverResult lists the MCP servers and skills found. It cannot carry a
// secret value: env and headers hold SecretField markers only.
type DiscoverResult struct {
	McpServers []DiscoveredMcp   `json:"mcpServers"`
	Skills     []DiscoveredSkill `json:"skills"`
}

// DiscoveredMcp is an MCP server definition found in the vendor configuration.
type DiscoveredMcp struct {
	Name      string                 `json:"name"`
	Scope     DiscoverScope          `json:"scope"`
	Project   string                 `json:"project,omitempty"`
	Source    string                 `json:"source,omitempty"`
	Transport McpTransport           `json:"transport"`
	Command   []string               `json:"command,omitempty"`
	URL       string                 `json:"url,omitempty"`
	Env       map[string]SecretField `json:"env,omitempty"`
	Headers   map[string]SecretField `json:"headers,omitempty"`
}

// DiscoveredSkill is a skill folder found on the machine.
type DiscoveredSkill struct {
	Name   string        `json:"name"`
	Scope  DiscoverScope `json:"scope"`
	Source string        `json:"source,omitempty"`
	Path   string        `json:"path"`
}

// SecretField marks an env var or header: either a secret whose value stays
// in the plugin, or a reference to an env var by name. A literal JSON string
// does not decode into it, so a plugin cannot pass a value through these maps.
type SecretField struct {
	Secret bool   `json:"secret,omitempty"`
	EnvRef string `json:"envRef,omitempty"`
}

// Validate checks that exactly one of Secret and EnvRef is set.
func (s SecretField) Validate() error {
	if s.Secret == (s.EnvRef != "") {
		return errors.New("protocol: exactly one of secret and envRef must be set")
	}
	return nil
}

// Validate checks names, scopes, transports and secret markers.
func (r DiscoverResult) Validate() error {
	var errs []error
	for i, m := range r.McpServers {
		p := fmt.Sprintf("mcpServers[%d]", i)
		if m.Name == "" {
			errs = append(errs, fmt.Errorf("protocol: %s.name is empty", p))
		}
		if !m.Scope.Valid() {
			errs = append(errs, fmt.Errorf("protocol: %s.scope %q is not valid", p, m.Scope))
		} else if m.Scope == DiscoverScopeProject && m.Project == "" {
			errs = append(errs, fmt.Errorf("protocol: %s.project is required for project scope", p))
		}
		switch m.Transport {
		case McpTransportStdio:
			if len(m.Command) == 0 {
				errs = append(errs, fmt.Errorf("protocol: %s.command is required for stdio", p))
			}
		case McpTransportHTTP, McpTransportSSE:
			if m.URL == "" {
				errs = append(errs, fmt.Errorf("protocol: %s.url is required for %s", p, m.Transport))
			}
		default:
			errs = append(errs, fmt.Errorf("protocol: %s.transport %q is not valid", p, m.Transport))
		}
		for k, f := range m.Env {
			if err := f.Validate(); err != nil {
				errs = append(errs, fmt.Errorf("%s.env[%s]: %w", p, k, err))
			}
		}
		for k, f := range m.Headers {
			if err := f.Validate(); err != nil {
				errs = append(errs, fmt.Errorf("%s.headers[%s]: %w", p, k, err))
			}
		}
	}
	for i, s := range r.Skills {
		p := fmt.Sprintf("skills[%d]", i)
		if s.Name == "" || s.Path == "" {
			errs = append(errs, fmt.Errorf("protocol: %s: name and path are required", p))
		}
		if !s.Scope.Valid() {
			errs = append(errs, fmt.Errorf("protocol: %s.scope %q is not valid", p, s.Scope))
		}
	}
	return errors.Join(errs...)
}
