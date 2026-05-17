package model

import "time"

// Pull request timeline event types.
const (
	PullEventRenamed    = "renamed"
	PullEventDescribed  = "described"
	PullEventLabeled    = "labeled"
	PullEventUnlabeled  = "unlabeled"
	PullEventAssigned   = "assigned"
	PullEventUnassigned = "unassigned"
	PullEventMerged     = "merged"
	PullEventClosed     = "closed"
	PullEventReopened   = "reopened"
	PullEventReadied    = "readied"
	PullEventDrafted    = "drafted"
)

// PullEvent is one entry in a pull request's activity timeline.
type PullEvent struct {
	ID        int64     `db:"id"         json:"id"`
	PullID    int64     `db:"pull_id"    json:"pull_id"`
	ActorID   int64     `db:"actor_id"   json:"actor_id"`
	ActorName string    `db:"actor_name" json:"actor_name"`
	Type      string    `db:"event_type" json:"event_type"`
	Detail    string    `db:"detail"     json:"detail"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}
