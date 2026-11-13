package view

import (
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// SecretTypes is the filter's order: the labels are the scanner's pattern ids, spelled for people.
var SecretTypes = []struct{ ID, Label string }{
	{"aws_access_key_id", "AWS access key ID"},
	{"private_key", "Private key"},
	{"cloudzilla_token", "Cloudzilla access token"},
	{"jwt_secret", "JWT signing secret"},
	{"generic_api_key", "Generic API key"},
}

func SecretTypeLabel(id string) string {
	for _, t := range SecretTypes {
		if t.ID == id {
			return t.Label
		}
	}
	return id
}

func SecretResolutionLabel(resolution string) string {
	switch resolution {
	case model.SecretResolutionRevoked:
		return "Revoked"
	case model.SecretResolutionFalsePositive:
		return "False positive"
	case model.SecretResolutionUsedInTests:
		return "Used in tests"
	}
	return resolution
}

// SecretHintDisplay spells the stored hint: the first and last four characters get an ellipsis between them, and a
// private key's key kind becomes its header line.
func SecretHintDisplay(secretType, hint string) string {
	if secretType == "private_key" {
		if hint == "PRIVATE" {
			return "-----BEGIN PRIVATE KEY-----"
		}
		return "-----BEGIN " + hint + " PRIVATE KEY-----"
	}
	if r := []rune(hint); len(r) == 8 {
		return string(r[:4]) + "…" + string(r[4:])
	}
	return hint
}

type SecretTypeOption struct {
	Value, Label string
	Selected     bool
}

type SecretAlertRow struct {
	Number          int
	URL             string
	TypeLabel       string
	Hint            string
	Closed          bool
	Blocked         bool
	AutoRevoked     bool
	Incomplete      bool
	ResolutionLabel string
	Location        string
	CommitShort     string
	CommitURL       string
	Pusher          string
	DetectedAt      time.Time
	ClosedBy        string
	ClosedAt        time.Time
}

type SecretAlertsData struct {
	BasePage
	Owner, RepoName string
	State           string
	Type            string
	OpenCount       int
	ClosedCount     int
	TypeOptions     []SecretTypeOption
	Rows            []SecretAlertRow
	OpenTabURL      string
	ClosedTabURL    string
	NewerURL        string
	OlderURL        string
}

type SecretSnippetView struct {
	Path        string
	CommitShort string
	FileURL     string
	Lines       []SecretSnippetViewLine
	// Gone is set when the commit or file is no longer in the repository.
	Gone bool
}

type SecretSnippetViewLine struct {
	Number              int
	Secret              bool
	Before, Mask, After string
}

type SecretEventView struct {
	Kind        string
	Actor       string
	ActorURL    string
	CommitShort string
	CommitURL   string
	Ref         string
	// Detail is a resolution label or "history scan".
	Detail string
	When   time.Time
}

type SecretLocationView struct {
	Text        string
	CommitShort string
	CommitURL   string
}

type SecretAlertData struct {
	BasePage
	Owner, RepoName string
	ListURL         string
	ActionURL       string
	Number          int
	TypeID          string
	TypeLabel       string
	Hint            string
	Closed          bool
	Blocked         bool
	AutoRevoked     bool
	Incomplete      bool
	// CanAllow is false for a Cloudzilla token: it is revoked, not allowed.
	CanAllow         bool
	ResolutionLabel  string
	CommitShort      string
	CommitURL        string
	Pusher           string
	Ref              string
	BlockedBy        string
	BlockedAt        time.Time
	DetectedAt       time.Time
	Snippet          SecretSnippetView
	HowToFix         []string
	Events           []SecretEventView
	Locations        []SecretLocationView
	OnDefaultBranch  bool
	DefaultBranch    string
	DefaultCommit    string
	DefaultCommitURL string
}

// SecretHowToFix is the numbered list on the alert page. A token is revoked by the instance itself, so its steps differ.
func SecretHowToFix(secretType string) []string {
	switch secretType {
	case "cloudzilla_token":
		return []string{
			"Cloudzilla deleted this token when it found it, so it no longer works. Create a new one under Settings if you still need it.",
			"Move the new token out of the repository, for example into a pipeline secret.",
		}
	case "private_key":
		return []string{
			"Remove the key from every server and account that trusts it, and generate a new pair. Deleting the file doesn't help: the key stays in history and in every clone.",
			"Keep the new private key out of the repository.",
			"Close this alert as Revoked.",
		}
	case "aws_access_key_id":
		return []string{
			"Deactivate the key in AWS IAM and issue a new one. Deleting the file doesn't help: the key stays in history and in every clone.",
			"Move the new key out of the repository, for example into a pipeline secret.",
			"Close this alert as Revoked.",
		}
	}
	return []string{
		"Rotate the secret with whatever issued it. Deleting the file doesn't help: the value stays in history and in every clone.",
		"Move the new value out of the repository, for example into a pipeline secret.",
		"Close this alert as Revoked.",
	}
}

func ShortSHA(sha string) string {
	return sha[:min(len(sha), 7)]
}
