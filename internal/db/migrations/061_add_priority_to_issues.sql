ALTER TABLE issues ADD COLUMN IF NOT EXISTS priority TEXT
    CHECK (priority IN ('P0', 'P1', 'P2', 'P3'));
