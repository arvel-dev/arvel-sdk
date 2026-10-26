// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"fmt"
	"net/url"
)

// SandboxInfo is the "sandbox" member of the initialize meta (PROTOCOL §5.1, §15).
type SandboxInfo struct {
	Mode      SandboxMode `json:"mode"`
	Proxy     string      `json:"proxy,omitempty"`
	Workspace string      `json:"workspace,omitempty"`
}

// Validate checks the mode and, when set, that the proxy is an http URL.
func (s SandboxInfo) Validate() error {
	if !s.Mode.Valid() {
		return fmt.Errorf("protocol: sandbox.mode %q is not valid", s.Mode)
	}
	if s.Proxy == "" {
		return nil
	}
	u, err := url.Parse(s.Proxy)
	if err != nil || u.Scheme != "http" || u.Host == "" {
		return fmt.Errorf("protocol: sandbox.proxy must be an http URL with a host")
	}
	return nil
}

// VendorPaths lists the paths outside the workspace the vendor CLI needs.
type VendorPaths struct {
	Read  []string `json:"read,omitempty"`
	Write []string `json:"write,omitempty"`
}

// ManifestSandbox is the "sandbox" block of arvel.json (PROTOCOL §3.2, §15).
type ManifestSandbox struct {
	VendorPaths   VendorPaths `json:"vendorPaths,omitempty"`
	VendorHosts   []string    `json:"vendorHosts,omitempty"`
	MacOSKeychain bool        `json:"macosKeychain,omitempty"`
}
