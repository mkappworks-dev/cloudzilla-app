CREATE TABLE IF NOT EXISTS notifications (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id)         ON DELETE CASCADE,
    actor_id    INTEGER NOT NULL REFERENCES users(id),
    actor_name  TEXT    NOT NULL DEFAULT '',
    type        TEXT    NOT NULL CHECK(type IN (
                    'issue_comment','pr_comment',
                    'issue_closed','issue_reopened',
                    'pr_merged','pr_closed','pr_opened'
                )),
    repo_id     INTEGER NOT NULL REFERENCES repositories(id)  ON DELETE CASCADE,
    repo_name   TEXT    NOT NULL DEFAULT '',
    owner_name  TEXT    NOT NULL DEFAULT '',
    subject_id  INTEGER NOT NULL,
    subject_url TEXT    NOT NULL DEFAULT '',
    read        BOOLEAN NOT NULL DEFAULT 0,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_notifications_user      ON notifications(user_id);
CREATE INDEX IF NOT EXISTS idx_notifications_user_read ON notifications(user_id, read);
