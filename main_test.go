package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cli/go-gh/v2"
	"github.com/lithammer/dedent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thombashi/gh-actionarmor/internal/pkg/common"
	"github.com/thombashi/gh-actionarmor/pkg/cmd"
)

func TestExecute(t *testing.T) {
	if os.Getenv("GITHUB_RUN_ID") == "" {
		t.Skip("skipping test; this test is intended to run on local environment")
	}

	a := assert.New(t)
	r := require.New(t)

	testCases := []struct {
		name             string
		workflowBody     []byte
		configBody       []byte
		wantRuntimeError bool
	}{
		{
			name: "with config: invalid workflow: include multiple errors",
			workflowBody: []byte(dedent.Dedent(
				`
				name: Test Workflow
				on: push
				jobs:
				  test:
				    runs-on: ubuntu-latest
				    steps:
				      - uses: actions/checkout@v4
				`)),
			configBody: []byte(dedent.Dedent(
				`
				exclude_official_actions: false
				enforce_pin_hash: true
				`)),
			wantRuntimeError: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()

			_, sderr, err := gh.Exec("repo", "clone", "thombashi/gh-actionarmor", tempDir, "--", "--depth=1")
			r.NoError(err, sderr.String())

			tempGhDir := filepath.Join(tempDir, ".github")
			tempGhWorkflowDir := filepath.Join(tempGhDir, "workflows")

			tempWorkflowFile, err := os.CreateTemp(tempGhWorkflowDir, "workflow-*.yaml")
			r.NoError(err)

			_, err = tempWorkflowFile.Write(tc.workflowBody)
			r.NoError(err)

			err = os.WriteFile(filepath.Join(tempGhDir, "actionarmor.yaml"), tc.configBody, 0755)
			r.NoError(err)

			os.Args = []string{common.ToolName, "--log-level=debug", tempDir}
			_, lintErrors := cmd.Execute()
			a.Len(lintErrors, 4)
		})
	}
}
