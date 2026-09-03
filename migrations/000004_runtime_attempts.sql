ALTER TABLE runs
    ADD COLUMN sequence INTEGER NOT NULL DEFAULT 1 CHECK (sequence > 0);

ALTER TABLE runs
    DROP CONSTRAINT IF EXISTS runs_job_id_attempt_key;

ALTER TABLE runs
    ADD CONSTRAINT runs_job_id_attempt_sequence_key UNIQUE (job_id, attempt, sequence);

CREATE INDEX runs_job_attempt_idx
    ON runs (job_id, attempt, sequence);
