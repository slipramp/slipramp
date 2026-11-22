-- +goose Up
LOCK TABLE run_server_gate_overrides, promotion_server_gate_overrides IN ACCESS EXCLUSIVE MODE;

CREATE TABLE server_gate_overrides (
    subject_type text NOT NULL CHECK (subject_type IN ('run', 'promotion')),
    subject_id uuid NOT NULL,
    run_id uuid GENERATED ALWAYS AS (CASE WHEN subject_type = 'run' THEN subject_id END) STORED REFERENCES runs (id),
    promotion_id uuid GENERATED ALWAYS AS (CASE WHEN subject_type = 'promotion' THEN subject_id END) STORED
        REFERENCES promotions (id),
    scope_id uuid NOT NULL,
    gate_type text NOT NULL,
    version bigint NOT NULL CHECK (version > 0),
    actor_id uuid NOT NULL REFERENCES principals (id),
    actor jsonb NOT NULL CHECK (jsonb_typeof(actor) = 'object'),
    reason text NOT NULL CHECK (length(reason) BETWEEN 1 AND 1024 AND btrim(reason) <> ''),
    recorded_at timestamptz NOT NULL,
    observed_state text NOT NULL,
    window_observation jsonb,
    CHECK (
        (gate_type = 'lock' AND window_observation IS NULL AND observed_state = 'blocking') OR
        (gate_type = 'window' AND window_observation IS NOT NULL AND jsonb_typeof(window_observation) = 'object'
          AND observed_state IN ('blocking','unavailable'))
    ),
    PRIMARY KEY (subject_type, subject_id, scope_id, gate_type, version),
    FOREIGN KEY (scope_id, gate_type, version) REFERENCES server_gate_scope_events (scope_id, gate_type, version)
);

INSERT INTO server_gate_overrides (subject_type, subject_id, scope_id, gate_type, version, actor_id, actor,
    reason, recorded_at, observed_state, window_observation)
SELECT subject_type, subject_id, scope_id, gate_type, version, actor_id, actor,
    reason, recorded_at, observed_state, window_observation
FROM subject_server_gate_overrides;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION run_server_gate_checks(checked_at timestamptz)
RETURNS TABLE(run_id uuid, scope_id uuid, gate_type text, version bigint, passing boolean, overridden boolean)
LANGUAGE sql STABLE AS $$
    SELECT r.id, c.scope_id, c.gate_type, c.version, c.passing,
        EXISTS (SELECT FROM server_gate_overrides o WHERE o.subject_type = 'run' AND o.subject_id = r.id
                AND (o.scope_id, o.gate_type, o.version) = (c.scope_id, c.gate_type, c.version))
    FROM runs r JOIN current_server_gate_checks(checked_at) c ON c.kind = 'global' OR c.target_id = r.target_id
    WHERE r.phase = 'queued' AND r.snapshot #>> '{action,Intent}' IN ('operation','reconcile');
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION promotion_server_gate_checks(checked_at timestamptz)
RETURNS TABLE(promotion_id uuid, scope_id uuid, gate_type text, version bigint, passing boolean, overridden boolean)
LANGUAGE sql STABLE AS $$
    SELECT p.id, c.scope_id, c.gate_type, c.version, c.passing,
        EXISTS (SELECT FROM server_gate_overrides o WHERE o.subject_type = 'promotion' AND o.subject_id = p.id
                AND (o.scope_id, o.gate_type, o.version) = (c.scope_id, c.gate_type, c.version))
    FROM waiting_promotion_gate_subjects p JOIN current_server_gate_checks(checked_at) c
        ON c.kind = 'global' OR c.stage_id = p.stage_id;
$$;
-- +goose StatementEnd

DROP VIEW subject_server_gate_overrides;
DROP TABLE run_server_gate_overrides, promotion_server_gate_overrides;

-- +goose Down
LOCK TABLE server_gate_overrides IN ACCESS EXCLUSIVE MODE;

CREATE TABLE run_server_gate_overrides (
    run_id uuid NOT NULL REFERENCES runs (id),
    scope_id uuid NOT NULL,
    version bigint NOT NULL CHECK (version > 0),
    actor_id uuid NOT NULL REFERENCES principals (id),
    actor jsonb NOT NULL CHECK (jsonb_typeof(actor) = 'object'),
    reason text NOT NULL CHECK (length(reason) BETWEEN 1 AND 1024 AND btrim(reason) <> ''),
    recorded_at timestamptz NOT NULL,
    gate_type text NOT NULL,
    observed_state text NOT NULL,
    window_observation jsonb,
    CONSTRAINT run_server_gate_override_shape CHECK (
        (gate_type = 'lock' AND window_observation IS NULL AND observed_state = 'blocking') OR
        (gate_type = 'window' AND window_observation IS NOT NULL AND jsonb_typeof(window_observation) = 'object'
          AND observed_state IN ('blocking','unavailable'))
    ),
    PRIMARY KEY (run_id, scope_id, gate_type, version),
    CONSTRAINT run_server_gate_override_event_fk FOREIGN KEY (scope_id, gate_type, version)
        REFERENCES server_gate_scope_events (scope_id, gate_type, version)
);

CREATE TABLE promotion_server_gate_overrides (
    promotion_id uuid NOT NULL REFERENCES promotions (id),
    scope_id uuid NOT NULL,
    version bigint NOT NULL CHECK (version > 0),
    actor_id uuid NOT NULL REFERENCES principals (id),
    actor jsonb NOT NULL CHECK (jsonb_typeof(actor) = 'object'),
    reason text NOT NULL CHECK (length(reason) BETWEEN 1 AND 1024 AND btrim(reason) <> ''),
    recorded_at timestamptz NOT NULL,
    gate_type text NOT NULL,
    observed_state text NOT NULL,
    window_observation jsonb,
    CONSTRAINT promotion_server_gate_override_shape CHECK (
        (gate_type = 'lock' AND window_observation IS NULL AND observed_state = 'blocking') OR
        (gate_type = 'window' AND window_observation IS NOT NULL AND jsonb_typeof(window_observation) = 'object'
          AND observed_state IN ('blocking','unavailable'))
    ),
    PRIMARY KEY (promotion_id, scope_id, gate_type, version),
    CONSTRAINT promotion_server_gate_override_event_fk FOREIGN KEY (scope_id, gate_type, version)
        REFERENCES server_gate_scope_events (scope_id, gate_type, version)
);

INSERT INTO run_server_gate_overrides (run_id, scope_id, gate_type, version, actor_id, actor,
    reason, recorded_at, observed_state, window_observation)
SELECT subject_id, scope_id, gate_type, version, actor_id, actor, reason, recorded_at, observed_state, window_observation
FROM server_gate_overrides WHERE subject_type = 'run';

INSERT INTO promotion_server_gate_overrides (promotion_id, scope_id, gate_type, version, actor_id, actor,
    reason, recorded_at, observed_state, window_observation)
SELECT subject_id, scope_id, gate_type, version, actor_id, actor, reason, recorded_at, observed_state, window_observation
FROM server_gate_overrides WHERE subject_type = 'promotion';

CREATE VIEW subject_server_gate_overrides AS
SELECT 'run'::text AS subject_type, run_id AS subject_id, scope_id, gate_type, version, observed_state, window_observation,
    actor_id, actor, reason, recorded_at FROM run_server_gate_overrides
UNION ALL
SELECT 'promotion'::text AS subject_type, promotion_id AS subject_id, scope_id, gate_type, version,
    observed_state, window_observation, actor_id, actor, reason, recorded_at FROM promotion_server_gate_overrides;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION run_server_gate_checks(checked_at timestamptz)
RETURNS TABLE(run_id uuid, scope_id uuid, gate_type text, version bigint, passing boolean, overridden boolean)
LANGUAGE sql STABLE AS $$
    SELECT r.id, c.scope_id, c.gate_type, c.version, c.passing,
        EXISTS (SELECT FROM run_server_gate_overrides o WHERE o.run_id = r.id
                AND (o.scope_id, o.gate_type, o.version) = (c.scope_id, c.gate_type, c.version))
    FROM runs r JOIN current_server_gate_checks(checked_at) c ON c.kind = 'global' OR c.target_id = r.target_id
    WHERE r.phase = 'queued' AND r.snapshot #>> '{action,Intent}' IN ('operation','reconcile');
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION promotion_server_gate_checks(checked_at timestamptz)
RETURNS TABLE(promotion_id uuid, scope_id uuid, gate_type text, version bigint, passing boolean, overridden boolean)
LANGUAGE sql STABLE AS $$
    SELECT p.id, c.scope_id, c.gate_type, c.version, c.passing,
        EXISTS (SELECT FROM promotion_server_gate_overrides o WHERE o.promotion_id = p.id
                AND (o.scope_id, o.gate_type, o.version) = (c.scope_id, c.gate_type, c.version))
    FROM waiting_promotion_gate_subjects p JOIN current_server_gate_checks(checked_at) c
        ON c.kind = 'global' OR c.stage_id = p.stage_id;
$$;
-- +goose StatementEnd

DROP TABLE server_gate_overrides;
