package session

import (
	"os/exec"
	"strings"
)

// currentGitBranch returns the current branch name in the given working
// directory, or "" if the directory is not a git repo or git is unavailable.
func currentGitBranch(cwd string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	branch := strings.TrimSpace(string(out))
	if branch == "HEAD" { // detached HEAD
		return ""
	}
	return branch
}
