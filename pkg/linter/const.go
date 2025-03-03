package linter

// ErrorKind represents a kind of lint error.
type ErrorKind string

const (
	KindArchivedActionUsed ErrorKind = "archived action action is being used"
	KindRuntimeError       ErrorKind = "runtime error"
	KindUnexpectedValue    ErrorKind = "unexpected value"
	KindUnpinned           ErrorKind = "must be pinned by hash"
)

var OfficialCreators = []string{
	"actions",
	"cli",
	"github",
}

// note: there are no APIs to get whether the action was created by a verified creator or not.
var actionsByVerifiedCreators = []string{
	"docker/login-action",
	"google-github-actions/auth",
	"google-github-actions/setup-gcloud",
	"slackapi/slack-github-action",
}
