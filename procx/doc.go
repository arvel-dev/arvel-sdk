// SPDX-License-Identifier: Apache-2.0

// Package procx holds the cross-platform process helpers every vendor plugin
// uses: finding a binary, probing its version, building a minimal environment,
// and starting and killing whole process trees on Linux, macOS and Windows.
//
// It never starts a shell, except cmd.exe for Windows .cmd and .bat shims.
package procx
