-- PAT scopes are now enforced. A token naming no known scope had full access, so
-- it gets every scope rather than losing git and API access on upgrade.
UPDATE access_tokens
SET scopes = 'repo:read,repo:write,issues:write,pulls:write'
WHERE NOT string_to_array(scopes, ',') && ARRAY['repo:read', 'repo:write', 'issues:write', 'pulls:write'];
