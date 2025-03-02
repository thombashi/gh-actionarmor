package git

import (
	"fmt"
	"strings"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/rhysd/actionlint"

	"github.com/thombashi/gh-taghash/pkg/resolver"
	"github.com/thombashi/go-gitexec"
)

var remoteCache = make(map[string]string)

// GetRepoID returns the repository ID (OWNER/NAME) of a Git repository from a actionlint.Project.
func GetRepoID(executor gitexec.GitExecutor, proj *actionlint.Project) (string, error) {
	if repoID, exist := remoteCache[proj.RootDir()]; exist {
		return repoID, nil
	}

	result, err := executor.RunGit("-C", proj.RootDir(), "config", "--get", "remote.origin.url")
	if err != nil {
		return "", fmt.Errorf("failed to get the remote origin URL: %w", err)
	}

	if result.ExitCode != 0 {
		return "", fmt.Errorf("failed to get the remote origin URL: %s", result.Stderr.String())
	}

	remoteURL := strings.TrimSpace(result.Stdout.String())
	repo, err := repository.Parse(remoteURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse the remote origin URL: %w", err)
	}

	repoID := resolver.ToRepoID(repo)
	remoteCache[proj.RootDir()] = repoID

	return repoID, nil
}
