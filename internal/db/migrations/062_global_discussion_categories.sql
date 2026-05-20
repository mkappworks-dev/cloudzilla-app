-- Discussion categories become a fixed, instance-wide set (no longer per-repo,
-- no maintainer CRUD). Existing per-repo categories — and the discussions that
-- reference them via ON DELETE CASCADE — are cleared so the seeded set takes over.
DELETE FROM discussion_categories;

ALTER TABLE discussion_categories DROP COLUMN repo_id;
ALTER TABLE discussion_categories DROP COLUMN emoji;
ALTER TABLE discussion_categories ADD COLUMN description TEXT NOT NULL DEFAULT '';
ALTER TABLE discussion_categories ADD CONSTRAINT discussion_categories_name_key UNIQUE (name);

INSERT INTO discussion_categories (name, description) VALUES
    ('General',       'Open conversation about anything related to the project.'),
    ('Q&A',           'Ask a question and let the community mark an answer.'),
    ('Ideas',         'Share and refine proposals before they become issues.'),
    ('Announcements', 'Updates and news from the maintainers.');
