-- strategy.fail-fast of the Job's matrix, copied onto each expanded Job so the scheduler needs no workflow file.
ALTER TABLE workflow_jobs ADD COLUMN fail_fast BOOLEAN NOT NULL DEFAULT TRUE;
