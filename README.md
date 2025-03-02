# gh-actionarmor

`gh-actionarmor` is a [gh][] extension for securing the use of GitHub actions in GitHub Actions workflows.

ref: [Security hardening for GitHub Actions - GitHub Docs](https://docs.github.com/en/actions/security-for-github-actions/security-guides/security-hardening-for-github-actions#using-third-party-actions)

[gh]: https://cli.github.com/


## Usage

### Command Help

```
gh-actionarmor lint actions of 'uses' in GitHub Actions workflows.

USAGE
  gh actionarmor [flags] [path ...]

  A path is either a directory path to a local GitHub repository or the path to a GitHub Actions workflows file.

RUN FLAGS:
      --config string      path to a config file.
                           if not specified, use default config file paths (.github/actionarmor.yaml or .github/actionarmor.yml)
      --log-level string   log level (debug, info, warn, error) (default "info")
  -n, --workers int        number of parallel workers. defaults to the number of CPUs in the system.

CACHE FLAGS:
      --cache-dir string   cache directory path. If not specified, use a user cache directory.
      --cache-ttl string   base cache TTL (time-to-live) (default "48h")
      --no-cache           disable cache

LINTER FLAGS:
      --action-allowlist strings    allowlist of actions (e.g. google-github-actions/auth). if specified, those actions are excluded from the linting.
      --allow-archived-repo         allow actions from archived repositories (default true)
      --creator-allowlist strings   allowlist of creators (e.g. google-github-actions). if specified, those creators are excluded from the linting.
      --enforce-pin-hash            enforce pinning a hash for actions (default true)
      --enforce-verified-org        enforce using actions from verified organizations
      --exclude-official            exclude actions created by official creators from linting. official creators are: actions, cli, github (default true)
      --exclude-verified-creators   exclude actions created by verified creators from linting
      --only-allowlisted-hash       allow only actions with a hash in the allowlist
```

### Configuration File
`gh-actionarmor` reads a configuration file named `actionarmor.yaml` or `actionarmor.yml` in the `.github` directory as a configuration file for linting.
The configuration file is written in YAML format as follows:

```yaml
# Same settings as flags of the same name
exclude_official_actions: true
exclude_verified_creators: true
allow_only_allowlisted_hash: false
allow_archived_repo: true
enforce_pin_hash: true
enforce_verified_org: false
creator_allowlist:
    - google

# Commit hash allowlist for actions
hash_allowlist:
    goreleaser/goreleaser-action:
        - sha: 7ec5c2b0c6cdda6e8bbb49444bc797dd33d74dd8
        - sha: 5742e2a039330cbb23ebf35f046f814d4c6ff811
```
