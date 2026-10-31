package library

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// Tool finds a helper program (ffmpeg, for video thumbnails): next to our executable first, so it can be shipped
// alongside file-presenter, then on PATH. Returns the bare name if not found so errors mention it.
func Tool(name string) string {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if exe, err := os.Executable(); err == nil {
		if p := filepath.Join(filepath.Dir(exe), name); FileExists(p) {
			return p
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return name
}

func FileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// command is exec.Command with a time limit (Quick Look can hang forever on files it can't read),
// minus the console window Windows would flash up for ffmpeg & co. Call the returned cancel when done.
func command(timeout time.Duration, name string, args ...string) (*exec.Cmd, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	c := exec.CommandContext(ctx, name, args...)
	c.WaitDelay = time.Second // don't wait on pipes held open by anything it started
	hideConsole(c)
	return c, cancel
}
