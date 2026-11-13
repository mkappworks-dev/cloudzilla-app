// Package runnerapi holds the JSON bodies of the runner protocol under /api/runner/, shared by the server's handlers
// and cz-runner so the two cannot drift.
package runnerapi

const (
	// HeartbeatEverySeconds is how often a runner reports in while it holds a Job; the server fails a Job after 60 s of silence.
	HeartbeatEverySeconds = 10
	// MaxClaimWaitSeconds caps the long poll of the claim endpoint.
	MaxClaimWaitSeconds = 30
)

type RegisterRequest struct {
	Name    string   `json:"name"`
	Labels  []string `json:"labels"`
	Version string   `json:"version"`
}

type RegisterResponse struct {
	RunnerID int64  `json:"runner_id"`
	Token    string `json:"token"`
}

// ClaimRequest may narrow the labels the runner offers to a subset of those it registered with.
type ClaimRequest struct {
	Labels []string `json:"labels"`
}

type StepSpec struct {
	Position         int               `json:"position"`
	Name             string            `json:"name"`
	Run              string            `json:"run"`
	Shell            string            `json:"shell"`
	WorkingDirectory string            `json:"working_directory"`
	Env              map[string]string `json:"env"`
	If               string            `json:"if"`
	TimeoutMinutes   int               `json:"timeout_minutes"`
}

type ArtifactSpec struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// JobSpec is one claimed Job: everything the runner needs, and no secret. Secrets are ticket 05's business and arrive
// as env there.
type JobSpec struct {
	JobID          int64             `json:"job_id"`
	RunID          int64             `json:"run_id"`
	RunNumber      int               `json:"run_number"`
	Name           string            `json:"name"`
	Repository     string            `json:"repository"`
	CloneURL       string            `json:"clone_url"`
	SHA            string            `json:"sha"`
	Ref            string            `json:"ref"`
	RunsOn         []string          `json:"runs_on"`
	Container      string            `json:"container"`
	TimeoutMinutes int               `json:"timeout_minutes"`
	Env            map[string]string `json:"env"`
	GitHub         map[string]string `json:"github"`
	Matrix         map[string]string `json:"matrix"`
	Needs          map[string]string `json:"needs"`
	Steps          []StepSpec        `json:"steps"`
	Artifacts      []ArtifactSpec    `json:"artifacts"`
	// Token is the per-job token: read-only, bound to this repo and Job, dead when the Job ends.
	Token string `json:"token"`
}

// HeartbeatResponse tells the runner whether to carry on; false means the Job was cancelled, finished or revoked.
type HeartbeatResponse struct {
	Continue bool `json:"continue"`
}

type StepRequest struct {
	Status string `json:"status"`
}

type FinishRequest struct {
	Result  string `json:"result"`
	Message string `json:"message"`
}
