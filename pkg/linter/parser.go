package linter

import (
	"fmt"
	"strings"

	"github.com/cli/go-gh/v2/pkg/repository"

	"github.com/thombashi/gh-taghash/pkg/resolver"
)

// Action represents information of 'uses' value of a GitHub Actions step.
type Action struct {
	// ID represents a unique ID of the action. It consists of 'owner', 'name', [directory]
	ID    string
	Owner string
	Name  string
	Ref   string
}

func (a Action) String() string {
	return fmt.Sprintf("%s/%s@%s", a.Owner, a.Name, a.Ref)
}

// RepoID returns a string of 'owner/repo'.
func (a Action) RepoID() string {
	return fmt.Sprintf("%s/%s", a.Owner, a.Name)
}

func (a Action) Repository() repository.Repository {
	return repository.Repository{
		Owner: a.Owner,
		Name:  a.Name,
	}
}

// IsLocalReusableWorkflows returns true if the action is a local reusable workflows.
// ref: https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#jobsjob_iduses
func (a Action) IsLocalReusableWorkflows() bool {
	return strings.HasPrefix(a.ID, "./.github/workflows/")
}

// IsPinnedBySHA returns true if the 'Ref' value is a SHA hash.
func (a Action) IsPinnedBySHA() bool {
	return resolver.IsSHA(a.Ref)
}

// ParseActionUses parses 'uses' value of a GitHub Actions step.
func ParseActionUses(uses string) (*Action, error) {
	uses = strings.TrimSpace(uses)
	items := strings.Split(uses, "@")
	if len(items) != 2 {
		return nil, fmt.Errorf("unexpected 'uses' value: %s", uses)
	}

	actionID := items[0]
	ref := items[1]

	a := strings.Split(actionID, "/")
	if len(a) < 2 {
		return nil, fmt.Errorf("invalid uses value: expected=owner/repo, actual=%s", actionID)
	}

	return &Action{
		ID:    actionID,
		Owner: a[0],
		Name:  a[1],
		Ref:   ref,
	}, nil
}
