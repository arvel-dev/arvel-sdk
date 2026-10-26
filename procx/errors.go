// SPDX-License-Identifier: Apache-2.0

package procx

import (
	"errors"
	"fmt"
)

var (
	// ErrNotFound is returned by FindBinary when the binary is not on the search path.
	ErrNotFound = errors.New("procx: binary not found")

	// ErrUnsupported is returned for an operation the platform cannot do, such as
	// Proc.Interrupt on Windows. It wraps errors.ErrUnsupported.
	ErrUnsupported = fmt.Errorf("procx: operation not supported on this platform: %w", errors.ErrUnsupported)

	// ErrNoVersion is returned by Version when the output holds no version number.
	ErrNoVersion = errors.New("procx: no version found in output")

	// ErrRelativePath is returned by Start for a program path that is not absolute.
	ErrRelativePath = errors.New("procx: path must be absolute")

	// ErrUnsafeArg is returned for an argument that cannot be passed safely to a
	// .cmd or .bat file through cmd.exe.
	ErrUnsafeArg = errors.New("procx: argument cannot be passed safely to a .cmd or .bat file")
)
