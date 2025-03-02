package linter

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/rhysd/actionlint"
	"github.com/shurcooL/githubv4"
	"github.com/thombashi/gh-actionarmor/pkg/workflow"
	"github.com/thombashi/gh-git-describe/pkg/executor"
	"github.com/thombashi/gh-taghash/pkg/resolver"
	"golang.org/x/crypto/sha3"
	"golang.org/x/sync/semaphore"
	"gopkg.in/yaml.v3"
)

const maxCommentLen = 20

const (
	DefaultExcludeOfficialActions   = true
	DefaultExcludeVerifiedCreators  = false
	DefaultAllowOnlyAllowlistedHash = false
	DefaultAllowArchivedRepo        = true
	DefaultEnforcePinHash           = true
	DefaultEnforceVerifiedOrg       = false
)

var reNewLines = regexp.MustCompile(`[\r\n\s]+`)

func boolPtr(v bool) *bool {
	return &v
}

func shortenHash(hash string) string {
	if !resolver.IsSHA(hash) {
		return hash
	}

	return hash[:7]
}

func replaceNewlines(s string) string {
	return reNewLines.ReplaceAllString(s, " ")
}

func truncateString(s string, len int) string {
	if utf8.RuneCountInString(s) <= len {
		return s
	}

	runes := []rune(s)
	return string(runes[:len]) + "..."
}

func fanIn(done <-chan interface{}, channels ...<-chan Result) <-chan Result {
	var wg sync.WaitGroup
	multiplexedStream := make(chan Result)

	multiplex := func(c <-chan Result) {
		defer wg.Done()
		for i := range c {
			select {
			case <-done:
				return
			case multiplexedStream <- i:
			}
		}
	}

	wg.Add(len(channels))
	for _, c := range channels {
		go multiplex(c)
	}

	go func() {
		wg.Wait()
		close(multiplexedStream)
	}()

	return multiplexedStream
}

type Error struct {
	LintError           actionlint.Error
	WorkflowAbsFilePath string
	Project             *actionlint.Project
}

// QueryParams is a set of parameters for a query.
type QueryParams struct {
	// GhClient is a GitHub GraphQL client. This client is used when the Client is nil.
	Client *api.GraphQLClient

	// Variables is a map of variables for the query.
	Variables map[string]interface{}
}

func query[T any](query *T, params *QueryParams) error {
	hash := sha3.Sum256([]byte(reflect.TypeOf(*query).String()))
	hashStr := fmt.Sprintf("%x", hash)

	if err := params.Client.Query(hashStr, query, params.Variables); err != nil {
		return fmt.Errorf("failed to execute a query: %w", err)
	}

	return nil
}

// WorkflowPos represents a position in a workflow file.
type WorkflowPos struct {
	Path string
	Pos  *actionlint.Pos
}

func (w WorkflowPos) String() string {
	return fmt.Sprintf("%s:%d:%d", w.Path, w.Pos.Line, w.Pos.Col)
}

type AllowedEntry struct {
	SHA     string  `yaml:"sha"`
	Comment *string `yaml:"comment,omitempty"`
}

// GlobalLintParams represents a set of lint parameters for global settings.
type GlobalLintParams struct {
	// NumWorkers is the number of workers for linters.
	NumWorkers int64
}

func (p GlobalLintParams) String() string {
	return fmt.Sprintf("workers=%d", p.NumWorkers)
}

// WorkflowLintParams represents a set of lint parameters for workflows of a GitHub repository.
type WorkflowLintParams struct {
	// ExcludeOfficialActions is a flag to exclude official actions from the linting target.
	// If true, the linter allows using official actions.
	ExcludeOfficialActions *bool `yaml:"exclude_official_actions,omitempty"`

	// ExcludeVerifiedCreators is a flag to exclude actions from verified creators from the linting target.
	// If true, the linter allows using actions from verified creators.
	ExcludeVerifiedCreators *bool `yaml:"exclude_verified_creators,omitempty"`

	// AllowOnlyAllowlistedHash is a flag to allow using actions only pinned by commit hash.
	// If true, the linter allows that only listed in the HashAllowlist
	AllowOnlyAllowlistedHash *bool `yaml:"allow_only_allowlisted_hash,omitempty"`

	// AllowArchivedRepo is a flag to allow using archived actions.
	// If true, the linter allows using archived actions.
	AllowArchivedRepo *bool `yaml:"allow_archived_repo,omitempty"`

	// EnforcePinActionHash is a flag to enforce pinning actions by commit hash.
	// If true, the linter enforces pinning actions by commit hash.
	EnforcePinHash *bool `yaml:"enforce_pin_hash,omitempty"`

	// EnforceVerifiedOrganization is a flag to enforce using actions from verified organizations.
	// If true, the linter enforces using actions from verified organizations.
	EnforceVerifiedOrganization *bool `yaml:"enforce_verified_organization,omitempty"`

	// CreatorAllowlist is a list of creators who are allowed to use their actions.
	// If it is not empty, the linter allows using actions from creators in the list without linting.
	CreatorAllowlist []string `yaml:"creator_allowlist,omitempty"`

	// ActionAllowlist is a list of actions that are allowed to use.
	// If it is not empty, the linter allows using actions in the list without linting.
	// e.g. google-github-actions/auth
	ActionAllowlist []string `yaml:"action_allowlist,omitempty"`

	// HashAllowlist is a list of commit hashes that are allowed to use.
	// key is a repository ID (OWNER/NAME) or an action ID.
	// value is an allowlist of commit hashes that are allowed to use.
	HashAllowlist map[string][]AllowedEntry `yaml:"hash_allowlist,omitempty"`
}

type WorkflowLintOption func(*WorkflowLintParams) error

func WithExcludeOfficialActions(v bool) WorkflowLintOption {
	return func(p *WorkflowLintParams) error {
		p.ExcludeOfficialActions = &v
		return nil
	}
}

func WithExcludeVerifiedCreators(v bool) WorkflowLintOption {
	return func(p *WorkflowLintParams) error {
		p.ExcludeVerifiedCreators = &v
		return nil
	}
}

func WithAllowOnlyAllowlistedHash(v bool) WorkflowLintOption {
	return func(p *WorkflowLintParams) error {
		p.AllowOnlyAllowlistedHash = &v
		return nil
	}
}

func WithAllowArchivedRepo(v bool) WorkflowLintOption {
	return func(p *WorkflowLintParams) error {
		p.AllowArchivedRepo = &v
		return nil
	}
}

func WithEnforcePinHash(v bool) WorkflowLintOption {
	return func(p *WorkflowLintParams) error {
		p.EnforcePinHash = &v
		return nil
	}
}

func WithEnforceVerifiedOrganization(v bool) WorkflowLintOption {
	return func(p *WorkflowLintParams) error {
		p.EnforceVerifiedOrganization = &v
		return nil
	}
}

func WithCreatorAllowlist(v []string) WorkflowLintOption {
	return func(p *WorkflowLintParams) error {
		for _, creator := range v {
			if !slices.Contains(p.CreatorAllowlist, creator) {
				p.CreatorAllowlist = append(p.CreatorAllowlist, creator)
			}
		}

		return nil
	}
}

func WithActionAllowlist(v []string) WorkflowLintOption {
	return func(p *WorkflowLintParams) error {
		for _, action := range v {
			if !slices.Contains(p.ActionAllowlist, action) {
				p.ActionAllowlist = append(p.ActionAllowlist, action)
			}
		}

		return nil
	}
}

func WithHashAllowlist(v map[string][]AllowedEntry) WorkflowLintOption {
	return func(p *WorkflowLintParams) error {
		p.HashAllowlist = v
		return nil
	}
}

func NewWorkflowLintParams(opts ...WorkflowLintOption) (*WorkflowLintParams, error) {
	var p WorkflowLintParams

	for _, opt := range opts {
		if err := opt(&p); err != nil {
			return nil, fmt.Errorf("failed to apply an option: %w", err)
		}
	}

	if p.ExcludeOfficialActions == nil {
		p.ExcludeOfficialActions = boolPtr(DefaultExcludeOfficialActions)
	}

	if p.ExcludeVerifiedCreators == nil {
		p.ExcludeVerifiedCreators = boolPtr(DefaultExcludeVerifiedCreators)
	}

	if p.AllowOnlyAllowlistedHash == nil {
		p.AllowOnlyAllowlistedHash = boolPtr(DefaultAllowOnlyAllowlistedHash)
	}

	if p.AllowArchivedRepo == nil {
		p.AllowArchivedRepo = boolPtr(DefaultAllowArchivedRepo)
	}

	if p.EnforcePinHash == nil {
		p.EnforcePinHash = boolPtr(DefaultEnforcePinHash)
	}

	if p.EnforceVerifiedOrganization == nil {
		p.EnforceVerifiedOrganization = boolPtr(DefaultEnforceVerifiedOrg)
	}

	if p.CreatorAllowlist == nil {
		p.CreatorAllowlist = []string{}
	}

	if p.ActionAllowlist == nil {
		p.ActionAllowlist = []string{}
	}

	if p.HashAllowlist == nil {
		p.HashAllowlist = map[string][]AllowedEntry{}
	}

	return &p, nil
}

func (p WorkflowLintParams) GetOptions() []WorkflowLintOption {
	opts := make([]WorkflowLintOption, 0)

	if p.ExcludeOfficialActions != nil {
		opts = append(opts, WithExcludeOfficialActions(*p.ExcludeOfficialActions))
	}

	if p.ExcludeVerifiedCreators != nil {
		opts = append(opts, WithExcludeVerifiedCreators(*p.ExcludeVerifiedCreators))
	}

	if p.AllowOnlyAllowlistedHash != nil {
		opts = append(opts, WithAllowOnlyAllowlistedHash(*p.AllowOnlyAllowlistedHash))
	}

	if p.AllowArchivedRepo != nil {
		opts = append(opts, WithAllowArchivedRepo(*p.AllowArchivedRepo))
	}

	if p.EnforcePinHash != nil {
		opts = append(opts, WithEnforcePinHash(*p.EnforcePinHash))
	}

	if p.EnforceVerifiedOrganization != nil {
		opts = append(opts, WithEnforceVerifiedOrganization(*p.EnforceVerifiedOrganization))
	}

	if len(p.CreatorAllowlist) > 0 {
		opts = append(opts, WithCreatorAllowlist(p.CreatorAllowlist))
	}

	if len(p.ActionAllowlist) > 0 {
		opts = append(opts, WithActionAllowlist(p.ActionAllowlist))
	}

	if len(p.HashAllowlist) > 0 {
		opts = append(opts, WithHashAllowlist(p.HashAllowlist))
	}

	return opts
}

func (p WorkflowLintParams) String() string {
	out, err := yaml.Marshal(p)
	if err != nil {
		return fmt.Sprintf("failed to marshal lint parameters: %v", err.Error())
	}

	return strings.TrimSpace(string(out))
}

func (p WorkflowLintParams) GetHashAllowlist(action Action) []AllowedEntry {
	if allowlist, exist := p.HashAllowlist[action.ID]; exist {
		return allowlist
	}

	if allowlist, exist := p.HashAllowlist[action.RepoID()]; exist {
		return allowlist
	}

	return nil
}

// WorkflowLintInfo represents the linting information of a workflow file.
type WorkflowLintInfo struct {
	// FilePath is an absolute path to the GitHub Actions workflow file.
	FilePath string

	// Project is a GitHub project that contains the workflow file.
	Project *actionlint.Project

	// Params is a set of parameters for linting the workflow.
	Params *WorkflowLintParams

	// RepoID is a repository ID (OWNER/NAME) of the project.
	RepoID string
}

// RelPath returns a relative path to the project root.
func (wf WorkflowLintInfo) RelPath() (string, error) {
	if wf.Project == nil {
		return wf.FilePath, nil
	}

	root := wf.Project.RootDir()
	relPath, err := filepath.Rel(root, wf.FilePath)
	if err != nil {
		return "", fmt.Errorf("failed to get relative path: root=%s, error=%w", root, err)
	}

	return relPath, nil
}

func (wf WorkflowLintInfo) String() string {
	return fmt.Sprintf("path=%s, repo=%s, params=%s", wf.FilePath, wf.RepoID, wf.Params)
}

// Linter is an interface to lint workflows.
type Linter interface {
	// LintWorkflow lints a workflow content.
	// content must be the body of the workflow file.
	LintWorkflow(done <-chan interface{}, globalLintParams GlobalLintParams, wfLintInfo WorkflowLintInfo, content []byte) ([]<-chan Result, error)

	// LintWorkflowContext lints a workflow content with a context.
	// content must be the body of the workflow file.
	LintWorkflowContext(ctx context.Context, done <-chan interface{}, globalLintParams GlobalLintParams, wfLintInfo WorkflowLintInfo, content []byte) ([]<-chan Result, error)

	// LintWorkflowFile lints a workflow file.
	LintWorkflowFile(done <-chan interface{}, globalLintParams GlobalLintParams, wfLintInfo WorkflowLintInfo) ([]<-chan Result, error)

	// LintWorkflowFileContext lints a workflow file with a context.
	LintWorkflowFileContext(ctx context.Context, done <-chan interface{}, globalLintParams GlobalLintParams, wfLintInfo WorkflowLintInfo) ([]<-chan Result, error)

	// LintWorkflowFiles lints workflow files.
	LintWorkflowFiles(globalLintParams GlobalLintParams, wfInfoList []WorkflowLintInfo) ([]*Error, error)

	// LintWorkflowFiles lints workflow files with a context.
	LintWorkflowFilesContext(ctx context.Context, globalLintParams GlobalLintParams, wfInfoList []WorkflowLintInfo) ([]*Error, error)
}

// NewLinter creates a new Linter instance.
func NewLinter(logger *slog.Logger, gqlClient *api.GraphQLClient, gdExecutor executor.Executor, resolver *resolver.Resolver) Linter {
	return &linter{
		logger:     logger,
		gqlClient:  gqlClient,
		gdExecutor: gdExecutor,
		resolver:   resolver,
	}
}

type linter struct {
	logger     *slog.Logger
	gqlClient  *api.GraphQLClient
	gdExecutor executor.Executor
	resolver   *resolver.Resolver
}

func (l linter) getQueryParams(variables map[string]interface{}) *QueryParams {
	return &QueryParams{
		Client:    l.gqlClient,
		Variables: variables,
	}
}

type Result struct {
	LintErrors   []*Error
	RuntimeError error
}

// LintWorkflow lints a workflow content.
// content must be the body of the workflow file.
func (l linter) LintWorkflow(done <-chan interface{}, globalLintParams GlobalLintParams, wfLintInfo WorkflowLintInfo, content []byte) ([]<-chan Result, error) {
	return l.LintWorkflowContext(context.Background(), done, globalLintParams, wfLintInfo, content)
}

// LintWorkflowContext lints a workflow content with a context.
// content must be the body of the workflow file.
func (l linter) LintWorkflowContext(ctx context.Context, done <-chan interface{}, globalLintParams GlobalLintParams, wfLintInfo WorkflowLintInfo, content []byte) ([]<-chan Result, error) {
	executorChannels := make([]<-chan Result, 0)

	workflow, parseErrors := actionlint.Parse(content)
	if len(parseErrors) > 0 {
		msgs := make([]string, 0, len(parseErrors))
		for _, e := range parseErrors {
			msgs = append(msgs, e.Error())
		}
		msg := strings.Join(msgs, "\n")

		resultStream := make(chan Result)

		go func() {
			defer close(resultStream)

			select {
			case <-done:
				return
			case resultStream <- Result{LintErrors: nil, RuntimeError: fmt.Errorf("failed to parse workflow: path=%s, msg=%s", wfLintInfo.FilePath, msg)}:
			}
		}()

		executorChannels = append(executorChannels, resultStream)

		return executorChannels, nil
	}

	logger := l.logger.With(slog.String("path", wfLintInfo.FilePath))
	logger.Debug("linting a workflow file",
		slog.String("name", workflow.Name.Value),
		slog.Any("global-lint-params", globalLintParams),
		slog.Any("workflow-lint-info", wfLintInfo),
	)

	sem := semaphore.NewWeighted(globalLintParams.NumWorkers)

	runLinter := func(done <-chan interface{}, step *actionlint.Step) <-chan Result {
		resultStream := make(chan Result)

		if err := sem.Acquire(ctx, 1); err != nil {
			resultStream <- Result{LintErrors: nil, RuntimeError: err}
		}

		go func() {
			defer close(resultStream)

			lintErrors := make([]*Error, 0)
			switch exec := step.Exec.(type) {
			case *actionlint.ExecAction:
				if lintError := l.lintJobUses(ctx, exec.Uses, wfLintInfo); lintError != nil {
					lintErrors = append(lintErrors, lintError)
				}
			}

			sem.Release(1)

			select {
			case <-done:
				return
			case resultStream <- Result{LintErrors: lintErrors, RuntimeError: nil}:
			}
		}()

		return resultStream
	}

	for name, job := range workflow.Jobs {
		logger.Debug("linting a job", slog.String("job", name))

		for _, step := range job.Steps {
			executorChannels = append(executorChannels, runLinter(done, step))
		}
	}

	return executorChannels, nil
}

func (l *linter) checkVerifiedOrg(login string, params *WorkflowLintParams) error {
	if params.EnforceVerifiedOrganization == nil || !*params.EnforceVerifiedOrganization {
		return nil
	}

	variables := map[string]interface{}{
		"login": githubv4.String(login),
	}

	var queryLoginUser struct {
		User struct {
			Login string
		} `graphql:"user(login: $login)"`
	}
	if err := query(&queryLoginUser, l.getQueryParams(variables)); err != nil {
		if !strings.Contains(err.Error(), "Could not resolve to") {
			return err
		}
	}

	if queryLoginUser.User.Login != "" {
		l.logger.Debug("skip organization verification",
			slog.String("login", queryLoginUser.User.Login),
			slog.String("reason", "user found"),
		)
		return nil
	}

	var queryLoginOrg struct {
		Organization struct {
			Login string
		} `graphql:"organization(login: $login)"`
	}
	if err := query(&queryLoginOrg, l.getQueryParams(variables)); err != nil {
		return fmt.Errorf("failed to execute a query: %w", err)
	}
	if queryLoginOrg.Organization.Login == "" {
		return fmt.Errorf("organization not found: %s", login)
	}

	var queryIsVerified struct {
		Organization struct {
			IsVerified bool
		} `graphql:"organization(login: $login)"`
	}
	variables = map[string]interface{}{
		"login": githubv4.String(login),
	}
	if err := query(&queryIsVerified, l.getQueryParams(variables)); err != nil {
		return fmt.Errorf("failed to execute a query: %w", err)
	}
	if !queryIsVerified.Organization.IsVerified {
		return fmt.Errorf("organization is not verified: %s", login)
	}

	return nil
}

func (l *linter) isArchivedAction(a Action) (bool, *time.Time, error) {
	var queryIsArchived struct {
		Repository struct {
			IsArchived bool
			ArchivedAt *time.Time
		} `graphql:"repository(owner: $owner, name: $name)"`
	}
	variables := map[string]interface{}{
		"owner": githubv4.String(a.Owner),
		"name":  githubv4.String(a.Name),
	}

	if err := query(&queryIsArchived, l.getQueryParams(variables)); err != nil {
		return false, nil, err
	}

	return queryIsArchived.Repository.IsArchived, queryIsArchived.Repository.ArchivedAt, nil
}

func (l linter) isVerifiedCreator(a Action) (bool, error) {
	if slices.Contains(actionsByVerifiedCreators, a.RepoID()) {
		return true, nil
	}

	return false, nil
}

func (l linter) isAllowlisted(action Action, params *WorkflowLintParams) (bool, string) {
	if slices.Contains(params.CreatorAllowlist, action.Owner) {
		return true, "allowlisted creator"
	}

	if slices.Contains(params.ActionAllowlist, action.RepoID()) {
		return true, "allowlisted action"
	}

	if !action.IsPinnedBySHA() {
		return false, "not allowlisted"
	}

	allowlist := params.GetHashAllowlist(action)
	for _, entry := range allowlist {
		if entry.SHA != action.Ref {
			continue
		}

		return true, "pinned by allowlisted hash"
	}

	return false, "not allowlisted"
}

func (l linter) resolveGitTagNamesFromSha(ctx context.Context, repo repository.Repository, ref string) ([]string, error) {
	gitTags, err := l.resolver.ResolveFromHashContext(ctx, repo, ref)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve git tags from a ref: repo=%s/%s, ref=%s, error=%w",
			repo.Owner, repo.Name, ref, err)
	}

	tagNames := make([]string, 0, len(gitTags))
	for _, gitTag := range gitTags {
		tagNames = append(tagNames, gitTag.Tag)
	}

	return tagNames, nil
}

func (l linter) lintJobUses(ctx context.Context, uses *actionlint.String, wfLintInfo WorkflowLintInfo) *Error {
	if uses == nil {
		return nil
	}

	logValidActionFound := func(logger *slog.Logger, reason string, args ...any) {
		logger.Debug("valid action found", append([]any{slog.String("reason", reason)}, args...)...)
	}

	l.logger.Debug("linting a step", slog.String("uses", uses.Value))

	items := strings.Split(uses.Value, "@")
	switch len(items) {
	case 1:
		// @ may not be included in the value.
		// For example, when using an action in the same repository or using a Docker Hub action
		// ref:
		// https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#example-using-an-action-in-the-same-repository-as-the-workflow
		return nil
	case 2:
		relPath, err := wfLintInfo.RelPath()
		if err != nil {
			return newLintError(
				fmt.Sprintf("failed to get relative path: %s", err.Error()),
				wfLintInfo.FilePath, wfLintInfo, uses.Pos, KindRuntimeError)
		}

		workflowPos := WorkflowPos{Path: relPath, Pos: uses.Pos}

		action, err := ParseActionUses(uses.Value)
		if err != nil {
			return newLintError(err.Error(), relPath, wfLintInfo, workflowPos.Pos, KindUnexpectedValue)
		}

		logger := l.logger.With(
			slog.String("workflow", fmt.Sprintf("%s/%s", wfLintInfo.RepoID, workflowPos.String())),
			slog.String("action", action.ID),
		)

		if action.IsLocalReusableWorkflows() {
			logger.Info("skip linting", slog.String("reason", "local reusable workflow"))
			return nil
		}

		params := wfLintInfo.Params

		if *params.ExcludeOfficialActions && slices.Contains(OfficialCreators, action.Owner) {
			logValidActionFound(logger, "official action")
			return nil
		}

		allowlisted, reason := l.isAllowlisted(*action, params)
		if allowlisted {
			if action.IsPinnedBySHA() {
				refShortHash := shortenHash(action.Ref)
				tagNames, err := l.resolveGitTagNamesFromSha(ctx, action.Repository(), action.Ref)
				if err != nil {
					return newLintError(
						fmt.Sprintf("failed to resolve git tags from sha: %s", err.Error()),
						relPath, wfLintInfo, workflowPos.Pos, KindRuntimeError)
				}

				logValidActionFound(logger, reason,
					slog.String("hash", refShortHash),
					slog.Any("tag", tagNames),
				)
			} else {
				gitTag, err := l.resolver.ResolveFromTagContext(ctx, action.Repository(), action.Ref)
				if err != nil {
					return newLintError(
						fmt.Sprintf("failed to resolve git tag: %s", err.Error()),
						relPath, wfLintInfo, workflowPos.Pos, KindRuntimeError)
				}

				logValidActionFound(logger, reason,
					slog.String("tag", gitTag.Tag),
					slog.String("hash", gitTag.CommitHash),
				)
			}

			return nil
		}

		if *params.ExcludeVerifiedCreators {
			verifiedDev, err := l.isVerifiedCreator(*action)
			if err != nil {
				return newLintError(
					fmt.Sprintf("failed to check if the creator is verified: %s", err.Error()),
					relPath, wfLintInfo, workflowPos.Pos, KindRuntimeError)
			}
			if verifiedDev {
				logValidActionFound(logger, "verified creator")
				return nil
			}
		}

		archived, archivedAt, err := l.isArchivedAction(*action)
		if err != nil {
			return newLintError(
				fmt.Sprintf("failed to check if the action is archived: %s", err.Error()),
				relPath, wfLintInfo, workflowPos.Pos, KindRuntimeError)
		}
		if archived {
			if !*params.AllowArchivedRepo {
				return newLintError(
					fmt.Sprintf("archived action found: repo=%s, archived-at=%s", action.RepoID(), archivedAt.Format("2006-01-02")),
					relPath, wfLintInfo, workflowPos.Pos, "archived action is not allowed")
			}

			logger.Warn("archived action found", slog.String("archived-at", archivedAt.String()))
		}

		err = l.checkVerifiedOrg(action.Owner, params)
		if err != nil {
			return newLintError(
				fmt.Sprintf("failed to check if the owner is verified: %s", err.Error()),
				relPath, wfLintInfo, workflowPos.Pos, KindRuntimeError)
		}

		refPos := &actionlint.Pos{
			Line: uses.Pos.Line,
			Col:  uses.Pos.Col + len(action.ID) + 1,
		}

		if action.IsPinnedBySHA() {
			tagNames, err := l.resolveGitTagNamesFromSha(ctx, action.Repository(), action.Ref)
			if err != nil {
				return newLintError(
					fmt.Sprintf("failed to resolve git tags: %s", err.Error()),
					relPath, wfLintInfo, refPos, KindRuntimeError)
			}
			tagsStr := strings.Join(tagNames, ", ")
			refShortHash := shortenHash(action.Ref)

			if *params.AllowOnlyAllowlistedHash {
				allowedEntries := wfLintInfo.Params.GetHashAllowlist(*action)
				allowlist := make([]string, 0, len(allowedEntries))

				for _, entry := range allowedEntries {
					var comment string
					if entry.Comment != nil {
						comment = strings.TrimSpace(replaceNewlines(*entry.Comment))
					}

					entryTagNames, err := l.resolveGitTagNamesFromSha(ctx, action.Repository(), entry.SHA)
					if err != nil {
						return newLintError(
							fmt.Sprintf("failed to resolve git tags: %s", err.Error()),
							relPath, wfLintInfo, refPos, KindRuntimeError)
					}
					entryTagsStr := strings.Join(entryTagNames, ", ")
					entryShortSHA := shortenHash(entry.SHA)

					var allowlistEntry string
					if comment == "" {
						allowlistEntry = fmt.Sprintf("%s(%s)", entryShortSHA, entryTagsStr)
					} else {
						allowlistEntry = fmt.Sprintf("%s(%s: %s)", entryShortSHA, entryTagsStr, truncateString(comment, maxCommentLen))
					}

					allowlist = append(allowlist, allowlistEntry)
				}

				return newLintError(
					fmt.Sprintf("invalid ref value: action=%s, sha=%s(%s), allowlist=%v", action.ID, refShortHash, tagsStr, allowlist),
					relPath, wfLintInfo, refPos, "SHA is not allowlisted",
				)
			}

			logValidActionFound(logger, reason,
				slog.String("hash", refShortHash),
				slog.Any("tag", tagNames),
			)

			return nil
		}

		if *params.EnforcePinHash {
			return newLintError(
				fmt.Sprintf("invalid ref value: action=%s, expected=SHA, actual=%s", action.RepoID(), action.Ref),
				relPath, wfLintInfo, refPos, KindUnpinned,
			)
		}

		logger.Warn("unpinned action found", slog.String("reason", "not pinned by hash"))

		return nil

	default:
		return newLintError(
			fmt.Sprintf("invalid uses value: %s", uses.Pos.String()),
			wfLintInfo.FilePath, wfLintInfo, uses.Pos, KindUnexpectedValue)
	}
}

// LintWorkflowFile lints a workflow file.
func (l linter) LintWorkflowFile(done <-chan interface{}, globalLintParams GlobalLintParams, wfLintInfo WorkflowLintInfo) ([]<-chan Result, error) {
	return l.LintWorkflowFileContext(context.Background(), done, globalLintParams, wfLintInfo)
}

// LintWorkflowFileContext lints a workflow file with a context.
func (l linter) LintWorkflowFileContext(ctx context.Context, done <-chan interface{}, globalLintParams GlobalLintParams, wfLintInfo WorkflowLintInfo) ([]<-chan Result, error) {
	bytes, err := os.ReadFile(wfLintInfo.FilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read a workflow file: path=%s, error=%w", wfLintInfo.FilePath, err)
	}

	return l.LintWorkflowContext(ctx, done, globalLintParams, wfLintInfo, bytes)
}

// LintWorkflowFiles lints workflow files.
func (l linter) LintWorkflowFiles(globalLintParams GlobalLintParams, wfLintInfoList []WorkflowLintInfo) ([]*Error, error) {
	return l.LintWorkflowFilesContext(context.Background(), globalLintParams, wfLintInfoList)
}

// LintWorkflowFiles lints workflow files with a context.
func (l linter) LintWorkflowFilesContext(ctx context.Context, globalLintParams GlobalLintParams, wfLintInfoList []WorkflowLintInfo) ([]*Error, error) {
	executorChannels := make([]<-chan Result, 0)
	done := make(chan interface{})
	defer close(done)

	lintErrors := make([]*Error, 0)

	for _, wfLintInfo := range wfLintInfoList {
		channels, err := l.LintWorkflowFileContext(ctx, done, globalLintParams, wfLintInfo)
		if err != nil {
			lintErrors = append(lintErrors, newLintError(err.Error(), wfLintInfo.FilePath, wfLintInfo, nil, KindRuntimeError))
		}

		executorChannels = append(executorChannels, channels...)
	}

	for result := range fanIn(done, executorChannels...) {
		if result.RuntimeError != nil {
			l.logger.Error("failed to lint", slog.Any("error", result.RuntimeError))
			continue
		}

		lintErrors = append(lintErrors, result.LintErrors...)
	}
	// TODO: sort errors

	return lintErrors, nil
}

func ReadLintOptions(c *workflow.ActionArmorConfigFile) ([]WorkflowLintOption, error) {
	if c == nil {
		return nil, fmt.Errorf("required a config file")
	}

	data, err := c.ReadFile()
	if err != nil {
		return nil, fmt.Errorf("failed to read the config file: path=%s, error=%w", c.FilePath(), err)
	}

	params, err := toLintParams(data)
	if err != nil {
		return nil, fmt.Errorf("failed to convert the config file: path=%s, error=%w", c.FilePath(), err)
	}

	return params.GetOptions(), nil
}

func toLintParams(data []byte) (*WorkflowLintParams, error) {
	var params WorkflowLintParams

	err := yaml.Unmarshal(data, &params)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal the config data: %w", err)
	}

	for action, allowlist := range params.HashAllowlist {
		for i := range allowlist {
			params.HashAllowlist[action][i].SHA = strings.TrimSpace(params.HashAllowlist[action][i].SHA)
		}
	}

	return &params, nil
}

func newLintError(msg, relPath string, wfLintInfo WorkflowLintInfo, pos *actionlint.Pos, kind ErrorKind) *Error {
	return &Error{
		LintError: actionlint.Error{
			Message:  msg,
			Filepath: relPath,
			Line:     pos.Line,
			Column:   pos.Col,
			Kind:     string(kind),
		},
		WorkflowAbsFilePath: wfLintInfo.FilePath,
		Project:             wfLintInfo.Project,
	}
}
