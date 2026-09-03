ALTER TABLE event_inbox
    ADD COLUMN processing_owner TEXT,
    ADD COLUMN processing_until TIMESTAMPTZ,
    ADD COLUMN process_attempt INTEGER NOT NULL DEFAULT 0 CHECK (process_attempt >= 0),
    ADD COLUMN next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE INDEX event_inbox_claim_idx
    ON event_inbox (next_attempt_at, received_at, id)
    WHERE processed_at IS NULL;

CREATE INDEX event_inbox_processing_expiry_idx
    ON event_inbox (processing_until)
    WHERE processed_at IS NULL AND processing_until IS NOT NULL;
