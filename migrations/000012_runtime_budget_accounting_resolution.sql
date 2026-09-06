CREATE OR REPLACE FUNCTION resolve_runtime_budget_reservation_after_usage()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    UPDATE runtime_budget_reservations AS reservation
       SET state = 'accounted',
           resolved_at = NEW.recorded_at
      FROM runs AS run
     WHERE run.id = NEW.run_id
       AND reservation.repository = NEW.repository
       AND reservation.run_id = run.job_id || '.' || run.sequence::text || '.attempt-' || run.attempt::text
       AND reservation.provider = NEW.provider
       AND reservation.model = NEW.model
       AND reservation.state = 'active';
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS runtime_usage_resolves_budget_reservation ON runtime_usage_ledger;
CREATE TRIGGER runtime_usage_resolves_budget_reservation
AFTER INSERT ON runtime_usage_ledger
FOR EACH ROW
EXECUTE FUNCTION resolve_runtime_budget_reservation_after_usage();
