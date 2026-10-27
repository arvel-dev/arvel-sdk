// SPDX-License-Identifier: Apache-2.0

package mockhost

import (
	"time"

	"github.com/arvel-dev/arvel-sdk/testkit/scenario"
)

// Driver drives one plugin binary without a testing.TB: every failure is
// returned as an error, and scenarios return structured results. It is the
// surface the conformance CLI uses.
type Driver struct {
	*core
}

// StartDriver launches a plugin binary and returns a Driver that drives it.
func StartDriver(binaryPath string, opts ...StartOption) (*Driver, error) {
	c, err := startCore(binaryPath, opts...)
	if err != nil {
		return nil, err
	}
	return &Driver{core: c}, nil
}

// Shutdown sends _arvel/shutdown, closes the plugin's stdin and waits for the
// plugin to exit, returning an error on failure or timeout.
func (d *Driver) Shutdown() error {
	return d.core.shutdown()
}

// ShutdownExit sends _arvel/shutdown and waits up to timeout for the plugin to
// exit, returning its exit code. A non-zero exit is not an error; the caller
// interprets the code.
func (d *Driver) ShutdownExit(timeout time.Duration) (int, error) {
	return d.core.shutdownExit(timeout)
}

// RunScenario runs one scenario against the driver and returns a structured
// result. The plugin process must already be started (see StartDriver).
func (d *Driver) RunScenario(sc *scenario.Scenario, opts ScenarioOptions) (ScenarioResult, error) {
	return d.core.runScenario(sc, opts)
}
