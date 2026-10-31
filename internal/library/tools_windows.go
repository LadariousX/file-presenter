package library

import (
	"os/exec"
	"syscall"
)

func hideConsole(c *exec.Cmd) {
	const createNoWindow = 0x08000000
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
