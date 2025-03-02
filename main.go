package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/thombashi/eoe"
	"github.com/thombashi/gh-actionarmor/pkg/cmd"
	"github.com/thombashi/gh-actionarmor/pkg/git"
)

func main() {
	env, lintErrors := cmd.Execute()

	for _, lerr := range lintErrors {
		repoID, err := git.GetRepoID(env.GitExecutor, lerr.Project)
		eoe.ExitOnError(err, env.EoeParams.WithMessage("failed to get the repository ID"))

		src, err := os.ReadFile(lerr.WorkflowAbsFilePath)
		if err != nil {
			env.Logger.Error("failed to read the workflow file", slog.Any("error", err))
			continue
		}

		lerr.LintError.Filepath = fmt.Sprintf("%s/%s", repoID, lerr.LintError.Filepath)
		lerr.LintError.PrettyPrint(os.Stderr, src)
	}
}
