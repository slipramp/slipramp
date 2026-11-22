-- +goose Up
ALTER TABLE promotions ADD COLUMN callback_released boolean NOT NULL DEFAULT false;

-- Historical approved callbacks without repository Gates could already have
-- received approval. Treat them as released when the receipt is unknowable.
UPDATE promotions p SET callback_released = true
WHERE (p.source = 'kargo' OR p.review_required)
  AND p.request_digest IS NOT NULL AND p.status = 'approved'
  AND NOT EXISTS (SELECT FROM promotion_gates g WHERE g.promotion_id = p.id AND g.released_at IS NULL);

CREATE TABLE run_server_gate_overrides (
    run_id uuid NOT NULL REFERENCES runs (id),
    scope_id uuid NOT NULL,
    lock_version bigint NOT NULL CHECK (lock_version > 0),
    actor_id uuid NOT NULL REFERENCES principals (id),
    actor jsonb NOT NULL CHECK (jsonb_typeof(actor) = 'object'),
    reason text NOT NULL CHECK (length(reason) BETWEEN 1 AND 1024 AND btrim(reason) <> ''),
    recorded_at timestamptz NOT NULL,
    PRIMARY KEY (run_id, scope_id, lock_version),
    FOREIGN KEY (scope_id, lock_version) REFERENCES server_gate_scope_events (scope_id, lock_version)
);

CREATE TABLE promotion_server_gate_overrides (
    promotion_id uuid NOT NULL REFERENCES promotions (id),
    scope_id uuid NOT NULL,
    lock_version bigint NOT NULL CHECK (lock_version > 0),
    actor_id uuid NOT NULL REFERENCES principals (id),
    actor jsonb NOT NULL CHECK (jsonb_typeof(actor) = 'object'),
    reason text NOT NULL CHECK (length(reason) BETWEEN 1 AND 1024 AND btrim(reason) <> ''),
    recorded_at timestamptz NOT NULL,
    PRIMARY KEY (promotion_id, scope_id, lock_version),
    FOREIGN KEY (scope_id, lock_version) REFERENCES server_gate_scope_events (scope_id, lock_version)
);

CREATE VIEW subject_lock_overrides AS
SELECT 'run'::text AS subject_type, run_id AS subject_id,
    scope_id, lock_version, actor_id, actor, reason, recorded_at
FROM run_server_gate_overrides
UNION ALL
SELECT 'promotion'::text AS subject_type, promotion_id AS subject_id,
    scope_id, lock_version, actor_id, actor, reason, recorded_at
FROM promotion_server_gate_overrides;

-- +goose Down
DROP VIEW subject_lock_overrides;
DROP TABLE promotion_server_gate_overrides;
DROP TABLE run_server_gate_overrides;
ALTER TABLE promotions DROP COLUMN callback_released;
