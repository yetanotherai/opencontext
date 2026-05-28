//go:build !linux && !darwin && !windows

package service

import (
	"fmt"
	"runtime"
)

func newPlatformManager() (Manager, error) {
	return nil, fmt.Errorf("daemon management is not supported on %s yet; run `oc daemon` in a terminal", runtime.GOOS)
}

func CheckLinger() (bool, string) { return false, "" }
