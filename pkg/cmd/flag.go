package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/lithammer/dedent"
	"github.com/spf13/pflag"
	"github.com/thombashi/gh-actionarmor/internal/pkg/common"
	"github.com/thombashi/gh-actionarmor/pkg/linter"
)

// flag names: linter
const (
	excludeOfficialActionsFlagName      = "exclude-official"
	excludeVerifiedCreatorsFlagName     = "exclude-verified-creators"
	allowOnlyAllowlistedHashFlagName    = "only-allowlisted-hash"
	allowArchivedRepoFlagName           = "allow-archived-repo"
	enforcePinHashFlagName              = "enforce-pin-hash"
	enforceVerifiedOrganizationFlagName = "enforce-verified-org"

	creatorAllowlistFlagName = "creator-allowlist"
	actionAllowlistFlagName  = "action-allowlist"
)

type RunFlags struct {
	ConfigFilePath string
	LogLevelStr    string
	NumWorkers     int64
}

type CacheFlags struct {
	CacheDirPath string
	CacheTTLStr  string
	NoCache      bool
}

type LinterFlags struct {
	ExcludeOfficialActions   bool
	ExcludeVerifiedCreators  bool
	AllowOnlyAllowlistedHash bool
	AllowArchivedRepo        bool
	EnforcePinHash           bool
	EnforceVerifiedOrg       bool

	CreatorAllowlist []string
	ActionAllowlist  []string
}

type Flags struct {
	RunFlags
	CacheFlags
	LinterFlags
}

// NamedFlagSet represents a named pflag.FlagSet
type NamedFlagSet struct {
	Name    string
	FlagSet *pflag.FlagSet
}

type NewFlagSetFunc func(*Flags) *NamedFlagSet

func NewRunFlagSet(flags *Flags) *NamedFlagSet {
	const name = "RUN FLAGS"

	flagSet := pflag.NewFlagSet(name, pflag.ExitOnError)

	flagSet.StringVar(
		&flags.ConfigFilePath,
		"config",
		"",
		strings.TrimSpace(dedent.Dedent(fmt.Sprintf(`
			path to a config file.
			if not specified, use default config file paths (%s or %s)`,
			filepath.Join(".github", fmt.Sprintf("%s.yaml", common.ToolName)),
			filepath.Join(".github", fmt.Sprintf("%s.yml", common.ToolName)),
		))),
	)
	flagSet.StringVar(
		&flags.LogLevelStr,
		"log-level",
		"info",
		"log level (debug, info, warn, error)",
	)
	flagSet.Int64VarP(
		&flags.NumWorkers,
		"workers",
		"n",
		0,
		"number of parallel workers. defaults to the number of CPUs in the system.",
	)

	return &NamedFlagSet{
		Name:    name,
		FlagSet: flagSet,
	}
}

func NewCacheFlagSet(flags *Flags) *NamedFlagSet {
	const name = "CACHE FLAGS"

	flagSet := pflag.NewFlagSet(name, pflag.ExitOnError)

	flagSet.StringVar(
		&flags.CacheDirPath,
		"cache-dir",
		"",
		"cache directory path. If not specified, use a user cache directory.",
	)
	flagSet.StringVar(
		&flags.CacheTTLStr,
		"cache-ttl",
		"48h",
		"base cache TTL (time-to-live)",
	)
	flagSet.BoolVar(
		&flags.NoCache,
		"no-cache",
		false,
		"disable cache",
	)

	return &NamedFlagSet{
		Name:    name,
		FlagSet: flagSet,
	}
}

func NewLinterFlagSet(flags *Flags) *NamedFlagSet {
	const name = "LINTER FLAGS"

	flagSet := pflag.NewFlagSet(name, pflag.ExitOnError)

	flagSet.BoolVar(
		&flags.ExcludeOfficialActions,
		excludeOfficialActionsFlagName,
		linter.DefaultExcludeOfficialActions,
		fmt.Sprintf("exclude actions created by official creators from linting. official creators are: %s", strings.Join(linter.OfficialCreators, ", ")),
	)
	flagSet.BoolVar(
		&flags.ExcludeVerifiedCreators,
		excludeVerifiedCreatorsFlagName,
		linter.DefaultExcludeVerifiedCreators,
		"exclude actions created by verified creators from linting",
	)
	flagSet.BoolVar(
		&flags.AllowOnlyAllowlistedHash,
		allowOnlyAllowlistedHashFlagName,
		linter.DefaultAllowOnlyAllowlistedHash,
		"allow only actions with a hash in the allowlist",
	)
	flagSet.BoolVar(
		&flags.AllowArchivedRepo,
		allowArchivedRepoFlagName,
		linter.DefaultAllowArchivedRepo,
		"allow actions from archived repositories",
	)
	flagSet.BoolVar(
		&flags.EnforcePinHash,
		enforcePinHashFlagName,
		linter.DefaultEnforcePinHash,
		"enforce pinning a hash for actions",
	)
	flagSet.BoolVar(
		&flags.EnforceVerifiedOrg,
		enforceVerifiedOrganizationFlagName,
		linter.DefaultEnforceVerifiedOrg,
		"enforce using actions from verified organizations",
	)

	flagSet.StringArrayVar(
		&flags.CreatorAllowlist,
		creatorAllowlistFlagName,
		[]string{},
		"allowlist of creators (e.g. google-github-actions). if specified, those creators are excluded from the linting.",
	)
	flagSet.StringArrayVar(
		&flags.ActionAllowlist,
		actionAllowlistFlagName,
		[]string{},
		"allowlist of actions (e.g. google-github-actions/auth). if specified, those actions are excluded from the linting.",
	)

	return &NamedFlagSet{
		Name:    name,
		FlagSet: flagSet,
	}
}

func NewFlags(toolName string, newFlagSetFuncs []NewFlagSetFunc) (*Flags, []string, error) {
	flags := &Flags{}

	flagSets := make([]*NamedFlagSet, 0, len(newFlagSetFuncs))
	for _, f := range newFlagSetFuncs {
		flagSets = append(flagSets, f(flags))
	}

	pflag.Usage = func() {
		msg := fmt.Sprintf(`
			gh-%s lint actions of 'uses' in GitHub Actions workflows.

			USAGE
			  gh %s [flags] [path ...]
			  
			  A path is either a directory path to a local GitHub repository or the path to a GitHub Actions workflows file.`,
			toolName, toolName)
		msg = dedent.Dedent(msg)
		msg = strings.TrimLeft(msg, "\n")
		fmt.Fprintln(os.Stderr, msg)

		for _, flagSet := range flagSets {
			fmt.Fprintf(os.Stderr, "\n%s:\n", flagSet.Name)
			flagSet.FlagSet.PrintDefaults()
		}
	}

	for _, f := range flagSets {
		pflag.CommandLine.AddFlagSet(f.FlagSet)
	}

	pflag.Parse()

	args := pflag.Args()
	if len(args) == 0 {
		args = append(args, ".")
	}

	if flags.NumWorkers <= 0 {
		flags.NumWorkers = int64(runtime.NumCPU())
	}

	return flags, args, nil
}
