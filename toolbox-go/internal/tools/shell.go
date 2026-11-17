package tools

import (
	"os"
	"os/exec"
)

// shell is how a command string reaches the system shell.
type shell struct {
	program string
	args    []string
}

// findShell picks the shell the same way pi-durable's Node environment does: an
// explicit path, then /bin/bash, then bash on PATH, then plain sh. The two paths
// must agree, or the same command would behave differently in each.
func findShell(custom string) (shell, bool) {
	if custom != "" {
		if exists(custom) {
			return bashShell(custom), true
		}
		return shell{}, false
	}
	if exists("/bin/bash") {
		return bashShell("/bin/bash"), true
	}
	if found, err := exec.LookPath("bash"); err == nil {
		return bashShell(found), true
	}
	return shell{program: "sh", args: []string{"-c"}}, true
}

func bashShell(path string) shell {
	return shell{program: path, args: []string{"-c"}}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
