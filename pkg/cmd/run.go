package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/phsym/console-slog"
	"github.com/spf13/pflag"
	"github.com/thombashi/eoe"
	"github.com/thombashi/gh-actionarmor/internal/pkg/common"
	"github.com/thombashi/gh-actionarmor/pkg/git"
	"github.com/thombashi/gh-actionarmor/pkg/linter"
	"github.com/thombashi/gh-actionarmor/pkg/workflow"
	gitdescribe "github.com/thombashi/gh-git-describe/pkg/executor"
	"github.com/thombashi/gh-taghash/pkg/resolver"
	"github.com/thombashi/go-gitexec"
)

var configCache = make(map[string]*linter.WorkflowLintParams)

func newLogger(level slog.Level) *slog.Logger {
	logger := slog.New(
		console.NewHandler(os.Stderr, &console.HandlerOptions{
			Level: level,
		}),
	)

	return logger
}

func toWorkflowLintOptionsFromFlags(flags LinterFlags, logger *slog.Logger) []linter.WorkflowLintOption {
	opts := make([]linter.WorkflowLintOption, 0)

	pflag.CommandLine.VisitAll(func(f *pflag.Flag) {
		if !f.Changed {
			return
		}

		logger.Debug("changed flag value found", slog.String("flag", f.Name), slog.String("value", f.Value.String()))

		switch f.Name {
		case excludeOfficialActionsFlagName:
			opts = append(opts, linter.WithExcludeOfficialActions(flags.ExcludeOfficialActions))

		case excludeVerifiedCreatorsFlagName:
			opts = append(opts, linter.WithExcludeVerifiedCreators(flags.ExcludeVerifiedCreators))

		case allowOnlyAllowlistedHashFlagName:
			opts = append(opts, linter.WithAllowOnlyAllowlistedHash(flags.AllowOnlyAllowlistedHash))

		case allowArchivedRepoFlagName:
			opts = append(opts, linter.WithAllowArchivedRepo(flags.AllowArchivedRepo))

		case enforcePinHashFlagName:
			opts = append(opts, linter.WithEnforcePinHash(flags.EnforcePinHash))

		case enforceVerifiedOrganizationFlagName:
			opts = append(opts, linter.WithEnforceVerifiedOrganization(flags.EnforceVerifiedOrg))

		case creatorAllowlistFlagName:
			opts = append(opts, linter.WithCreatorAllowlist(flags.CreatorAllowlist))

		case actionAllowlistFlagName:
			opts = append(opts, linter.WithActionAllowlist(flags.ActionAllowlist))
		}
	})

	return opts
}

func makeLintParams(config *workflow.ActionArmorConfigFile, flags LinterFlags, logger *slog.Logger) (*linter.WorkflowLintParams, error) {
	var err error
	opts := make([]linter.WorkflowLintOption, 0)

	if config != nil {
		if params, exist := configCache[config.Hash()]; exist {
			logger.Debug("found a cached lint config",
				slog.String("dir", config.DirPath()),
				slog.String("file", config.FileName()),
				slog.String("hash", config.Hash()),
			)
			return params, nil
		}

		logger.Debug("reading a config file", slog.String("path", config.FilePath()))
		o, err := linter.ReadLintOptions(config)
		if err != nil {
			return nil, err
		}

		logger.Debug("red config file", slog.Int("options", len(o)))

		opts = append(opts, o...)
	} else {
		logger.Debug("config file is not specified. using the default lint parameters.")
	}

	o := toWorkflowLintOptionsFromFlags(flags, logger)
	opts = append(opts, o...)

	params, err := linter.NewWorkflowLintParams(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create a new WorkflowLintParams: %w", err)
	}

	if config != nil {
		configCache[config.Hash()] = params
	}

	return params, nil
}

// ToWorkflowLintInfo converts a list of WorkflowInfo to a list of WorkflowLintInfo.
func ToWorkflowLintInfo(
	wfInfoList []*workflow.WorkflowInfo,
	config *workflow.ActionArmorConfigFile,
	gitExecutor gitexec.GitExecutor,
	flags LinterFlags,
) ([]linter.WorkflowLintInfo, error) {
	wfLintInfoList := make([]linter.WorkflowLintInfo, 0, len(wfInfoList))

	for _, wfInfo := range wfInfoList {
		tmpConfig := config
		if tmpConfig == nil {
			tmpConfig = wfInfo.Config
		}

		params, err := makeLintParams(tmpConfig, flags, gitExecutor.GetLogger())
		if err != nil {
			return nil, err
		}

		repoID, err := git.GetRepoID(gitExecutor, wfInfo.Project)
		if err != nil {
			return nil, fmt.Errorf("failed to get the repository ID: %w", err)
		}

		wfLintInfoList = append(wfLintInfoList, linter.WorkflowLintInfo{
			FilePath: wfInfo.FilePath,
			Project:  wfInfo.Project,
			Params:   params,
			RepoID:   repoID,
		})
	}

	return wfLintInfoList, nil
}

type Environment struct {
	Logger      *slog.Logger
	EoeParams   *eoe.ExitOnErrorParams
	GqlClient   *api.GraphQLClient
	GitExecutor gitexec.GitExecutor
	GdExecutor  gitdescribe.Executor
	Linter      linter.Linter
}

func NewEnvironment(ctx context.Context, logLevel slog.Level, flags *CacheFlags) (*Environment, error) {
	logger := newLogger(logLevel)
	eoeParams := eoe.NewParams().WithLogger(logger).WithContext(ctx)

	cacheTTL, err := resolver.ParseCacheTTL(flags.CacheTTLStr)
	eoe.ExitOnError(err, eoeParams.WithMessage("failed to parse a cache TTL"))

	if flags.NoCache {
		cacheTTL.QueryTTL = 0
	}

	gqlClient, err := api.NewGraphQLClient(api.ClientOptions{
		CacheTTL: cacheTTL.QueryTTL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create a GitHub client: %w", err)
	}

	gitExecutor, err := gitexec.New(&gitexec.Params{
		Logger: logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create a git executer: %w", err)
	}

	gdExecutor, err := gitdescribe.New(&gitdescribe.Params{
		Logger:         logger,
		LogWithPackage: true,
		CacheDirPath:   flags.CacheDirPath,
		CacheTTL:       cacheTTL.GitFileTTL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create a git-describe executor: %w", err)
	}

	r, err := resolver.New(&resolver.Params{
		Client:          gqlClient,
		GitDescExecutor: gdExecutor,
		Logger:          logger,
		LogWithPackage:  true,
		CacheDirPath:    flags.CacheDirPath,
		ClearCache:      flags.NoCache,
		CacheTTL:        *cacheTTL,
	})
	eoe.ExitOnError(err, eoeParams.WithMessage("failed to create a resolver"))

	linter := linter.NewLinter(logger, gqlClient, gdExecutor, r)

	return &Environment{
		Logger:      logger,
		EoeParams:   eoeParams,
		GqlClient:   gqlClient,
		GitExecutor: gitExecutor,
		GdExecutor:  gdExecutor,
		Linter:      linter,
	}, nil
}

func Execute() (*Environment, []*linter.Error) {
	var err error
	var config *workflow.ActionArmorConfigFile

	flags, args, err := NewFlags(common.ToolName, []NewFlagSetFunc{
		NewRunFlagSet,
		NewCacheFlagSet,
		NewLinterFlagSet,
	})
	eoe.ExitOnError(err, eoe.NewParams().WithMessage("failed to set flags"))

	var logLevel slog.Level
	err = logLevel.UnmarshalText([]byte(flags.LogLevelStr))
	eoe.ExitOnError(err, eoe.NewParams().WithMessage("failed to get a slog level"))

	ctx := context.Background()

	env, err := NewEnvironment(ctx, logLevel, &flags.CacheFlags)
	eoe.ExitOnError(err, env.EoeParams.WithMessage("failed to create an environment"))

	wfInfoList, err := workflow.ListWorkflows(args, env.Logger)
	eoe.ExitOnError(err, env.EoeParams.WithMessage("failed to list workflow file paths"))

	// 再帰的に ListWorkflows を行う関数

	if flags.ConfigFilePath != "" {
		config = workflow.NewConfigFileFromFile(flags.ConfigFilePath)
	}

	wfLintInfoList, err := ToWorkflowLintInfo(wfInfoList, config, env.GitExecutor, flags.LinterFlags)
	eoe.ExitOnError(err, env.EoeParams.WithMessage("failed to convert workflow info"))

	globalLintParams := linter.GlobalLintParams{
		NumWorkers: flags.NumWorkers,
	}

	env.Logger.Debug("linter process parameters", slog.String("global", globalLintParams.String()))

	lintErrors, err := env.Linter.LintWorkflowFilesContext(ctx, globalLintParams, wfLintInfoList)
	eoe.ExitOnError(err, env.EoeParams.WithMessage("failed to lint"))

	return env, lintErrors
}
