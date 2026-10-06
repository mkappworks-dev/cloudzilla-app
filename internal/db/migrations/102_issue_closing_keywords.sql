-- keyword rows mirror the closing references in the PR's title and body and are
-- rewritten on every edit; manual rows are only ever changed by hand.
ALTER TABLE pull_issue_links
    ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'keyword'));

CREATE TABLE IF NOT EXISTS issue_events (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    issue_id   BIGINT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    actor_id   BIGINT NOT NULL REFERENCES users(id),
    actor_name TEXT NOT NULL,
    event_type TEXT NOT NULL,
    pull_id    BIGINT REFERENCES pull_requests(id) ON DELETE SET NULL,
    commit_sha TEXT,
    -- The repo the PR or the pushed commit belongs to, which a cross-repo close
    -- needs to link back to.
    source_repo_id BIGINT REFERENCES repositories(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_issue_events_issue ON issue_events (issue_id, created_at);

-- A PR or a commit closes an issue at most once, even after the issue is reopened.
CREATE UNIQUE INDEX IF NOT EXISTS uq_issue_events_closed_by_pull
    ON issue_events (issue_id, pull_id) WHERE event_type = 'closed' AND pull_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_issue_events_closed_by_commit
    ON issue_events (issue_id, commit_sha) WHERE event_type = 'closed' AND commit_sha IS NOT NULL;

ALTER TABLE pull_requests
    ADD COLUMN IF NOT EXISTS auto_merge_by BIGINT REFERENCES users(id) ON DELETE SET NULL;
