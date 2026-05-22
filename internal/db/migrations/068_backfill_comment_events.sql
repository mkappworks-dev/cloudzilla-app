-- Backfill activity-feed events for existing issue and pull-request comments.
-- Push events cannot be backfilled (Git keeps no log of when refs moved), so
-- only comments are replayed here, preserving their original timestamps.

INSERT INTO events (actor_id, actor_name, repo_id, repo_name, owner_name, event_type, payload, created_at)
SELECT
    c.author_id,
    u.username,
    i.repo_id,
    r.name,
    r.owner_name,
    'comment',
    jsonb_build_object(
        'number', i.number,
        'kind', 'issue',
        'body', CASE WHEN char_length(c.body) > 280
                     THEN left(c.body, 280) || '…'
                     ELSE c.body END
    ),
    c.created_at
FROM comments c
JOIN issues i       ON i.id = c.issue_id
JOIN repositories r ON r.id = i.repo_id AND r.deleted_at IS NULL
JOIN users u        ON u.id = c.author_id
WHERE c.issue_id IS NOT NULL;

INSERT INTO events (actor_id, actor_name, repo_id, repo_name, owner_name, event_type, payload, created_at)
SELECT
    c.author_id,
    u.username,
    p.repo_id,
    r.name,
    r.owner_name,
    'comment',
    jsonb_build_object(
        'number', p.number,
        'kind', 'pull',
        'body', CASE WHEN char_length(c.body) > 280
                     THEN left(c.body, 280) || '…'
                     ELSE c.body END
    ),
    c.created_at
FROM comments c
JOIN pull_requests p ON p.id = c.pull_id
JOIN repositories r  ON r.id = p.repo_id AND r.deleted_at IS NULL
JOIN users u         ON u.id = c.author_id
WHERE c.pull_id IS NOT NULL;
