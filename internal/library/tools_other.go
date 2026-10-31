//go:build !windows

package library

import "os/exec"

func hideConsole(*exec.Cmd) {}
