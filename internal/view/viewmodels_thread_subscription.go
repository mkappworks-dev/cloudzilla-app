package view

// ThreadSubscriptionData drives the sidebar Notifications control on issue, pull request and discussion pages.
type ThreadSubscriptionData struct {
	Owner    string
	RepoName string
	// Segment is the URL collection: issues, pulls or discussions.
	Segment string
	Number  int
	// State is empty when the viewer is not subscribed.
	State  string
	Reason string
}
