-- The ghost takes over what a deleted account wrote in other people's repos
-- (UserStore.DeleteWithOwnedRepos). It has no password or linked identity, and
-- lookups by name or email skip it. An existing account already named ghost
-- keeps the name; the ghost then takes the next free ghost<n>.
DO $$
DECLARE
  uname TEXT := 'ghost';
  n     INT  := 1;
  gid   BIGINT;
BEGIN
  WHILE EXISTS (SELECT 1 FROM users WHERE lower(username) = uname OR lower(email) = uname || '@ghost.invalid')
     OR EXISTS (SELECT 1 FROM organizations WHERE lower(name) = uname) LOOP
    n := n + 1;
    uname := 'ghost' || n;
  END LOOP;

  INSERT INTO users (username, email, password_hash, name, bio, email_notifications)
  VALUES (uname, uname || '@ghost.invalid', '', 'Ghost', 'Stands in for deleted accounts.', FALSE)
  RETURNING id INTO gid;

  -- A constant, so it is IMMUTABLE and index predicates can use it.
  EXECUTE format('CREATE FUNCTION ghost_user_id() RETURNS BIGINT LANGUAGE sql IMMUTABLE PARALLEL SAFE AS %L',
                 format('SELECT %s::bigint', gid));
END $$;

CREATE FUNCTION users_keep_ghost() RETURNS trigger AS $$
BEGIN
  IF OLD.id = ghost_user_id() THEN
    RAISE EXCEPTION 'the ghost user stands in for deleted accounts and cannot be deleted';
  END IF;
  RETURN OLD;
END $$ LANGUAGE plpgsql;
CREATE TRIGGER users_keep_ghost
  BEFORE DELETE ON users
  FOR EACH ROW EXECUTE FUNCTION users_keep_ghost();

-- One review per reviewer, except the ghost, which holds every deleted reviewer's.
ALTER TABLE pull_reviews DROP CONSTRAINT pull_reviews_pull_id_author_id_key;
CREATE UNIQUE INDEX pull_reviews_pull_id_author_id_key ON pull_reviews (pull_id, author_id)
  WHERE author_id <> ghost_user_id();
