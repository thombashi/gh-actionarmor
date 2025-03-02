package linter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseActionUses(t *testing.T) {
	a := assert.New(t)

	testCases := []struct {
		Uses       string
		Want       *Action
		WantErrStr string
	}{
		{
			Uses: "owner/repo@ref",
			Want: &Action{
				ID:    "owner/repo",
				Owner: "owner",
				Name:  "repo",
				Ref:   "ref",
			},
			WantErrStr: "",
		},
		{
			Uses: "octo-org/this-repo/.github/workflows/workflow-1.yml@172239021f7ba04fe7327647b213799853a9eb89",
			Want: &Action{
				ID:    "octo-org/this-repo/.github/workflows/workflow-1.yml",
				Owner: "octo-org",
				Name:  "this-repo",
				Ref:   "172239021f7ba04fe7327647b213799853a9eb89",
			},
			WantErrStr: "",
		},
		{
			Uses: "octo-org/another-repo/.github/workflows/workflow.yml@v1",
			Want: &Action{
				ID:    "octo-org/another-repo/.github/workflows/workflow.yml",
				Owner: "octo-org",
				Name:  "another-repo",
				Ref:   "v1",
			},
			WantErrStr: "",
		},
		{
			Uses:       "invalid",
			Want:       nil,
			WantErrStr: "unexpected 'uses' value: invalid",
		},
		{
			Uses:       "invalid@ref",
			Want:       nil,
			WantErrStr: "invalid uses value: expected=owner/repo, actual=invalid",
		},
		{
			Uses:       "owner/repo@ref@ref",
			Want:       nil,
			WantErrStr: "unexpected 'uses' value: owner/repo@ref@ref",
		},
	}

	for _, test := range testCases {
		got, err := ParseActionUses(test.Uses)
		a.Equal(test.Want, got)
		if err != nil {
			a.Equal(test.WantErrStr, err.Error())
		}
	}
}

func TestActionString(t *testing.T) {
	a := assert.New(t)

	action := Action{
		Owner: "octocat",
		Name:  "hello-world",
		Ref:   "v1.0.0",
	}

	want := "octocat/hello-world@v1.0.0"
	got := action.String()

	a.Equal(want, got)
}

func TestActionRepoID(t *testing.T) {
	a := assert.New(t)

	action := Action{
		Owner: "octocat",
		Name:  "hello-world",
		Ref:   "v1.0.0",
	}

	want := "octocat/hello-world"
	got := action.RepoID()

	a.Equal(want, got)
}

func TestIsLocalReusableWorkflows(t *testing.T) {
	a := assert.New(t)

	testCases := []struct {
		ID   string
		Want bool
	}{
		{
			ID:   "./.github/workflows/ci.yml",
			Want: true,
		},
		{
			ID:   "owner/repo",
			Want: false,
		},
		{
			ID:   "owner/repo@ref",
			Want: false,
		},
		{
			ID:   "octo-org/this-repo/.github/workflows/workflow-1.yml@172239021f7ba04fe7327647b213799853a9eb89",
			Want: false,
		},
		{
			ID:   "octo-org/another-repo/.github/workflows/workflow.yml@v1",
			Want: false,
		},
	}

	for _, tc := range testCases {
		action := Action{
			ID: tc.ID,
		}
		got := action.IsLocalReusableWorkflows()
		a.Equal(tc.Want, got)
	}
}

func TestIsPinnedBySHA(t *testing.T) {
	a := assert.New(t)

	testCases := []struct {
		Ref  string
		Want bool
	}{
		{
			Ref:  "0123456789abcdef0123456789abcdef01234567",
			Want: true,
		},
		{
			Ref:  "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
			Want: true,
		},
		{
			Ref:  "v1.0.0",
			Want: false,
		},
		{
			Ref:  "main",
			Want: false,
		},
	}

	for _, tc := range testCases {
		action := Action{Ref: tc.Ref}
		got := action.IsPinnedBySHA()
		a.Equal(tc.Want, got)
	}
}
