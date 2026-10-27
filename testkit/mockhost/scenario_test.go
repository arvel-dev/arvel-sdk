// SPDX-License-Identifier: Apache-2.0

package mockhost

import "testing"

func TestRunScenario_InProcess(t *testing.T) {
	h := InProcess(t, streamingProvider())
	h.RunScenario(t, "testdata/provider-basic.yaml")
	h.Shutdown()
}

func TestRunScenario_Permission(t *testing.T) {
	h := InProcess(t, permissionProvider())
	h.RunScenario(t, "testdata/provider-permission.yaml")
	h.Shutdown()
}
