-- Application code deletes a repo's grants when it leaves the team's organization (RepoStore.UpdateOwner,
-- RepoTransferStore.Accept); nothing in the schema ties repo_id to teams.org_id.
CREATE TABLE IF NOT EXISTS team_repos (
    team_id    BIGINT      NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    repo_id    BIGINT      NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    role       TEXT        NOT NULL CHECK (role IN ('reader', 'writer', 'admin')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (team_id, repo_id)
);

CREATE INDEX IF NOT EXISTS idx_team_repos_repo ON team_repos (repo_id);
