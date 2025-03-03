package linter

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/lithammer/dedent"
	"github.com/phsym/console-slog"
	"github.com/rhysd/actionlint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gitdescribe "github.com/thombashi/gh-git-describe/pkg/executor"
	"github.com/thombashi/gh-taghash/pkg/resolver"
)

var testLogger = slog.New(
	console.NewHandler(os.Stderr, &console.HandlerOptions{
		Level: slog.LevelDebug,
	}),
)

// func TestMain(m *testing.M) {
// 	goleak.VerifyTestMain(m)
// }

func TestWorkflowPos_String(t *testing.T) {
	pos := &WorkflowPos{
		Path: "/path/to/workflow.yml",
		Pos: &actionlint.Pos{
			Line: 10,
			Col:  5,
		},
	}
	want := "/path/to/workflow.yml:10:5"
	got := pos.String()

	assert.Equal(t, want, got)
}

func TestLintWorkflowContext(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)

	logger := testLogger.With(slog.String("test", "TestLintWorkflowContext"))
	cacheTTL := resolver.NewCacheTTL(60 * time.Second)
	gqlClient, err := api.NewGraphQLClient(api.ClientOptions{
		CacheTTL: cacheTTL.QueryTTL,
	})
	r.NoError(err)

	gdExecutor, err := gitdescribe.New(&gitdescribe.Params{
		Logger:         logger,
		LogWithPackage: true,
		CacheTTL:       cacheTTL.GitFileTTL,
	})
	r.NoError(err)

	resolver, err := resolver.New(&resolver.Params{
		Client:          gqlClient,
		GitDescExecutor: gdExecutor,
		Logger:          logger,
		CacheDirPath:    t.TempDir(),
		ClearCache:      true,
		CacheTTL:        *cacheTTL,
	})
	r.NoError(err)

	l := NewLinter(logger, gqlClient, gdExecutor, resolver)
	assertEqualError := func(t *testing.T, want, got *Error) {
		t.Helper()

		wantLE := want.LintError
		gotLE := got.LintError

		a.Contains(gotLE.Message, wantLE.Message, gotLE.Message)
		a.Equal(wantLE.Line, gotLE.Line)
		a.Equal(wantLE.Column, gotLE.Column)
		a.Equal(wantLE.Kind, gotLE.Kind)
	}

	testCases := []struct {
		name             string
		workflowBody     []byte
		lintInfo         *WorkflowLintInfo
		wantRuntimeError bool
		wantLintErrors   []*Error
	}{
		{
			name: "valid workflow: official action",
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
			lintInfo: &WorkflowLintInfo{
				Params: func() *WorkflowLintParams {
					p, err := NewWorkflowLintParams()
					r.NoError(err)
					return p
				}(),
				RepoID: "owner/repo",
			},
			wantRuntimeError: false,
			wantLintErrors:   nil,
		},
		{
			name: "valid workflow: pinned by hash",
			workflowBody: []byte(dedent.Dedent(
				`
				name: Test Workflow
				on: push
				jobs:
				  test:
				    runs-on: ubuntu-latest
				    steps:
				      - uses: tj-actions/changed-files@d6e91a2266cdb9d62096cebf1e8546899c6aa18f  # v45
				`)),
			lintInfo: &WorkflowLintInfo{
				Params: func() *WorkflowLintParams {
					p, err := NewWorkflowLintParams(WithEnforcePinHash(true))
					r.NoError(err)
					return p
				}(),
				RepoID: "owner/repo",
			},
			wantRuntimeError: false,
			wantLintErrors:   nil,
		},
		{
			name: "valid workflow: set creator allowlist",
			workflowBody: []byte(dedent.Dedent(
				`
				name: Test Workflow
				on: push
				jobs:
				  test:
				    runs-on: ubuntu-latest
				    steps:
				    - uses: 'actions/checkout@v4'
				    - uses: 'google-github-actions/auth@v2'
				      with:
				        project_id: 'my-project'
				        workload_identity_provider: 'projects/123456789/locations/global/workloadIdentityPools/my-pool/providers/my-provider'
				    - uses: 'google-github-actions/setup-gcloud@v2'
				      with:
				        version: '>= 363.0.0'
				`)),
			lintInfo: &WorkflowLintInfo{
				Params: func() *WorkflowLintParams {
					p, err := NewWorkflowLintParams(
						WithEnforcePinHash(true),
						WithCreatorAllowlist([]string{"google-github-actions"}),
					)
					r.NoError(err)
					return p
				}(),
				RepoID: "owner/repo",
			},
			wantRuntimeError: false,
			wantLintErrors:   nil,
		},
		{
			name: "invalid workflow: set action allowlist",
			workflowBody: []byte(dedent.Dedent(
				`
				name: Test Workflow
				on: push
				jobs:
				  test:
				    runs-on: ubuntu-latest
				    steps:
				    - uses: 'actions/checkout@v4'
				    - uses: 'google-github-actions/auth@v2'
				      with:
				        project_id: 'my-project'
				        workload_identity_provider: 'projects/123456789/locations/global/workloadIdentityPools/my-pool/providers/my-provider'
				    - uses: 'google-github-actions/setup-gcloud@v2'
				      with:
				        version: '>= 363.0.0'
				`)),
			lintInfo: &WorkflowLintInfo{
				Params: func() *WorkflowLintParams {
					p, err := NewWorkflowLintParams(
						WithEnforcePinHash(true),
						WithActionAllowlist([]string{"google-github-actions/auth"}),
					)
					r.NoError(err)
					return p
				}(),
				RepoID: "owner/repo",
			},
			wantRuntimeError: false,
			wantLintErrors: []*Error{
				{
					LintError: actionlint.Error{
						Message: "invalid ref value: action=google-github-actions/setup-gcloud, expected=SHA, actual=v2",
						Line:    13,
						Column:  48,
						Kind:    string(KindUnpinned),
					},
				},
			},
		},
		{
			name: "valid workflow: pinned by a hash that is in allowlist",
			workflowBody: []byte(dedent.Dedent(
				`
				name: Test Workflow
				on: push
				jobs:
				  test:
				    runs-on: ubuntu-latest
				    steps:
				      - uses: tj-actions/changed-files@d6e91a2266cdb9d62096cebf1e8546899c6aa18f  # v45
				`)),
			lintInfo: &WorkflowLintInfo{
				Params: func() *WorkflowLintParams {
					p, err := NewWorkflowLintParams(
						WithEnforcePinHash(true),
						WithAllowOnlyAllowlistedHash(true),
						WithHashAllowlist(map[string][]AllowedEntry{
							"tj-actions/changed-files": {
								{
									SHA: "d6e91a2266cdb9d62096cebf1e8546899c6aa18f",
								},
							},
						}),
					)
					r.NoError(err)
					return p
				}(),
				RepoID: "owner/repo",
			},
			wantRuntimeError: false,
			wantLintErrors:   nil,
		},
		{
			name: "invalid: include official actions",
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
			lintInfo: &WorkflowLintInfo{
				Params: func() *WorkflowLintParams {
					p, err := NewWorkflowLintParams(WithExcludeOfficialActions(false))
					r.NoError(err)
					return p
				}(),
				RepoID: "owner/repo",
			},
			wantRuntimeError: false,
			wantLintErrors: []*Error{
				{
					LintError: actionlint.Error{
						Message: "invalid ref value: action=actions/checkout, expected=SHA, actual=v4",
						Line:    8,
						Column:  32,
						Kind:    string(KindUnpinned),
					},
				},
			},
		},
		{
			name: "invalid workflow: not pinned by hash",
			workflowBody: []byte(dedent.Dedent(
				`
				name: Test Workflow
				on: push
				jobs:
				  test:
				    runs-on: ubuntu-latest
				    steps:
				      - uses: tj-actions/changed-files@v45
				`)),
			lintInfo: &WorkflowLintInfo{
				Params: func() *WorkflowLintParams {
					p, err := NewWorkflowLintParams(WithEnforcePinHash(true))
					r.NoError(err)
					return p
				}(),
				RepoID: "owner/repo",
			},
			wantRuntimeError: false,
			wantLintErrors: []*Error{
				{
					LintError: actionlint.Error{
						Message: "invalid ref value: action=tj-actions/changed-files, expected=SHA, actual=v45",
						Line:    8,
						Column:  40,
						Kind:    string(KindUnpinned),
					},
				},
			},
		},
		{
			name: "invalid workflow: not pinned by hash when enforce pin hash for official actions",
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
			lintInfo: &WorkflowLintInfo{
				Params: func() *WorkflowLintParams {
					p, err := NewWorkflowLintParams(
						WithEnforcePinHash(true),
						WithExcludeOfficialActions(false),
					)
					r.NoError(err)
					return p
				}(),
				RepoID: "owner/repo",
			},
			wantRuntimeError: false,
			wantLintErrors: []*Error{
				{
					LintError: actionlint.Error{
						Message: "invalid ref value: action=actions/checkout, expected=SHA, actual=v",
						Line:    8,
						Column:  32,
						Kind:    string(KindUnpinned),
					},
				},
			},
		},
		{
			name: "invalid workflow: invalid pinned hash",
			workflowBody: []byte(dedent.Dedent(
				`
				name: Test Workflow
				on: push
				jobs:
				  test:
				    runs-on: ubuntu-latest
				    steps:
				      - uses: tj-actions/changed-files@1234567890123456789012345678901234567890
				`)),
			lintInfo: &WorkflowLintInfo{
				Params: func() *WorkflowLintParams {
					p, err := NewWorkflowLintParams(WithEnforcePinHash(true))
					r.NoError(err)
					return p
				}(),
				RepoID: "owner/repo",
			},
			wantRuntimeError: false,
			wantLintErrors: []*Error{
				{
					LintError: actionlint.Error{
						Message: "1234567890123456789012345678901234567890 is neither a commit nor blob",
						Line:    8,
						Column:  40,
						Kind:    string(KindRuntimeError),
					},
				},
			},
		},
		{
			name: "invalid workflow: archived official action",
			workflowBody: []byte(dedent.Dedent(
				`
				name: Test Workflow
				on: push
				jobs:
				  test:
				    runs-on: ubuntu-latest
				    steps:
				      - uses: actions/create-release@v1
				`)),
			lintInfo: &WorkflowLintInfo{
				Params: func() *WorkflowLintParams {
					p, err := NewWorkflowLintParams(
						WithExcludeOfficialActions(true),
						WithAllowArchivedRepo(false),
					)
					r.NoError(err)
					return p
				}(),
				RepoID: "owner/repo",
			},
			wantRuntimeError: false,
			wantLintErrors: []*Error{
				{
					LintError: actionlint.Error{
						Message: "archived action found: repo=actions/create-release, archived-at=2021-03-04",
						Line:    8,
						Column:  15,
						Kind:    string(KindArchivedActionUsed),
					},
				},
			},
		},
		{
			name: "invalid workflow: include multiple errors",
			workflowBody: []byte(dedent.Dedent(
				`
				name: Test Workflow
				on: push
				jobs:
				  test:
				    runs-on: ubuntu-latest
				    steps:
				      - uses: tj-actions/changed-files@1234567890123456789012345678901234567890
				      - uses: bufbuild/buf-action@v1.0.2
				`)),
			lintInfo: &WorkflowLintInfo{
				Params: func() *WorkflowLintParams {
					p, err := NewWorkflowLintParams(WithEnforcePinHash(true))
					r.NoError(err)
					return p
				}(),
				RepoID: "owner/repo",
			},
			wantRuntimeError: false,
			wantLintErrors: []*Error{
				{
					LintError: actionlint.Error{
						Message: "invalid ref value: action=bufbuild/buf-action, expected=SHA, actual=v1.0.2",
						Line:    9,
						Column:  35,
						Kind:    string(KindUnpinned),
					},
				},
				{
					LintError: actionlint.Error{
						Message: "1234567890123456789012345678901234567890 is neither a commit nor blob",
						Line:    8,
						Column:  40,
						Kind:    string(KindRuntimeError),
					},
				},
			},
		},
		{
			name:         "invalid workflow: empty workflow",
			workflowBody: []byte(``),
			lintInfo: &WorkflowLintInfo{
				Params: func() *WorkflowLintParams {
					p, err := NewWorkflowLintParams()
					r.NoError(err)
					return p
				}(),
				RepoID: "owner/repo",
			},
			wantRuntimeError: true,
		},
		{
			name: "invalid workflow: malformed workflow",
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
			lintInfo: &WorkflowLintInfo{
				Params: func() *WorkflowLintParams {
					p, err := NewWorkflowLintParams()
					r.NoError(err)
					return p
				}(),
				RepoID: "owner/repo",
			},
			wantRuntimeError: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			tempGhWorkflowDir := filepath.Join(tempDir, ".github", "workflows")
			r.NoError(os.MkdirAll(tempGhWorkflowDir, 0755))

			tempFile, err := os.CreateTemp(tempGhWorkflowDir, "workflow-*.yaml")
			r.NoError(err)

			_, err = tempFile.Write(tc.workflowBody)
			r.NoError(err)

			lintErrors := make([]*Error, 0)
			done := make(chan interface{})
			defer close(done)

			tc.lintInfo.FilePath = tempFile.Name()

			globalLintParams := GlobalLintParams{
				NumWorkers: int64(runtime.NumCPU()),
			}

			executorChannels, err := l.LintWorkflowFileContext(context.Background(), done, globalLintParams, *tc.lintInfo)
			r.NoError(err)

			for result := range fanIn(done, executorChannels...) {
				if result.RuntimeError != nil {
					t.Log(string(tc.workflowBody))
				}
				if tc.wantRuntimeError {
					a.Error(result.RuntimeError)
					return
				}
				r.NoError(result.RuntimeError)

				lintErrors = append(lintErrors, result.LintErrors...)
			}

			a.Len(lintErrors, len(tc.wantLintErrors))
			if len(lintErrors) != len(tc.wantLintErrors) {
				for _, lerr := range lintErrors {
					logger.Error("show error", slog.Any("error", lerr.LintError.Error()))
				}

				t.FailNow()
			}

			for i, want := range tc.wantLintErrors {
				assertEqualError(t, want, lintErrors[i])
			}
		})
	}
}
