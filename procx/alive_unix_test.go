// SPDX-License-Identifier: Apache-2.0

//go:build unix

package procx

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// processAlive reports whether pid is a running process. A zombie counts as
// dead: in containers without an init process orphans are never reaped.
func processAlive(pid int) bool {
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return false
	}
	if data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil {
		s := string(data)
		if i := strings.LastIndexByte(s, ')'); i >= 0 && i+2 < len(s) {
			return s[i+2] != 'Z'
		}
		return true
	}
	out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return false // ps found no such process
	}
	return !strings.HasPrefix(strings.TrimSpace(string(out)), "Z")
}
