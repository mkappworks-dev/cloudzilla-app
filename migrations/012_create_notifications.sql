CREATE TABLE IF NOT EXISTS notifications (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     BIGINT  NOT NULL REFERENCES users(id)         ON DELETE CASCADE,
    actor_id    BIGINT  NOT NULL REFERENCES users(id),
    actor_name  TEXT    NOT NULL DEFAULT '',
    type        TEXT    NOT NULL CHECK(type IN (
                    'issue_comment','pr_comment',
                    'issue_closed','issue_reopened',
                    'pr_merged','pr_closed','pr_opened'
                )),
    repo_id     BIGINT  NOT NULL REFERENCES repositories(id)  ON DELETE CASCADE,
    repo_name   TEXT    NOT NULL DEFAULT '',
    owner_name  TEXT    NOT NULL DEFAULT '',
    subject_id  BIGINT  NOT NULL,
    subject_url TEXT    NOT NULL DEFAULT '',
    read        BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_notifications_user      ON notifications(user_id);
CREATE INDEX IF NOT EXISTS idx_notifications_user_read ON notifications(user_id, read);
