-- +goose Up
ALTER TABLE server_gate_scopes
    ADD COLUMN window_version bigint NOT NULL DEFAULT 0 CHECK (window_version >= 0),
    ADD COLUMN window_definition jsonb CHECK (jsonb_typeof(window_definition) = 'object'),
    ADD COLUMN window_digest text CHECK (window_digest ~ '^[0-9a-f]{64}$'),
    ADD COLUMN window_state text CHECK (window_state IN ('passing','blocking')),
    ADD COLUMN window_evaluated_at timestamptz,
    ADD COLUMN window_valid_until timestamptz,
    ADD COLUMN window_interval_start timestamptz,
    ADD COLUMN window_next_transition timestamptz,
    ADD CHECK (num_nonnulls(window_definition, window_digest, window_state, window_evaluated_at, window_valid_until) IN (0,5)),
    ADD CHECK (window_definition IS NULL OR (window_version > 0 AND window_valid_until > window_evaluated_at)),
    ADD CHECK (window_interval_start IS NULL OR (window_state IS NOT DISTINCT FROM 'passing'
        AND window_interval_start <= window_evaluated_at)),
    ADD CHECK (window_next_transition IS NULL OR (window_valid_until IS NOT NULL AND window_next_transition = window_valid_until));

CREATE INDEX server_gate_windows_due ON server_gate_scopes (window_valid_until)
    WHERE window_definition IS NOT NULL;

DROP VIEW subject_lock_overrides;
ALTER TABLE run_server_gate_overrides DROP CONSTRAINT run_server_gate_overrides_scope_id_lock_version_fkey;
ALTER TABLE promotion_server_gate_overrides DROP CONSTRAINT promotion_server_gate_overrides_scope_id_lock_version_fkey;

ALTER TABLE server_gate_scope_events RENAME COLUMN lock_version TO version;
ALTER TABLE server_gate_scope_events
    DROP CONSTRAINT server_gate_scope_events_pkey,
    DROP CONSTRAINT server_gate_scope_events_action_check,
    ALTER COLUMN reason DROP NOT NULL,
    ADD COLUMN gate_type text NOT NULL DEFAULT 'lock',
    ADD COLUMN window_definition jsonb CHECK (jsonb_typeof(window_definition) = 'object'),
    ADD COLUMN window_digest text CHECK (window_digest ~ '^[0-9a-f]{64}$'),
    ADD CONSTRAINT server_gate_event_shape CHECK (
        (gate_type = 'lock' AND action IN ('lock','unlock') AND reason IS NOT NULL AND window_definition IS NULL AND window_digest IS NULL)
        OR (gate_type = 'window' AND reason IS NULL AND expires_at IS NULL AND
            ((action = 'set' AND window_definition IS NOT NULL AND window_digest IS NOT NULL)
              OR (action = 'remove' AND window_definition IS NULL AND window_digest IS NULL)))),
    ADD PRIMARY KEY (scope_id, gate_type, version);
ALTER TABLE server_gate_scope_events ALTER COLUMN gate_type DROP DEFAULT;

ALTER TABLE run_server_gate_overrides RENAME COLUMN lock_version TO version;
ALTER TABLE run_server_gate_overrides
    DROP CONSTRAINT run_server_gate_overrides_pkey,
    ADD COLUMN gate_type text NOT NULL DEFAULT 'lock',
    ADD COLUMN observed_state text NOT NULL DEFAULT 'blocking',
    ADD COLUMN window_observation jsonb,
    ADD CONSTRAINT run_server_gate_override_shape CHECK (
        (gate_type = 'lock' AND window_observation IS NULL AND observed_state = 'blocking') OR
        (gate_type = 'window' AND window_observation IS NOT NULL AND jsonb_typeof(window_observation) = 'object'
          AND observed_state IN ('blocking','unavailable'))),
    ADD PRIMARY KEY (run_id, scope_id, gate_type, version),
    ADD CONSTRAINT run_server_gate_override_event_fk FOREIGN KEY (scope_id, gate_type, version)
        REFERENCES server_gate_scope_events (scope_id, gate_type, version);
ALTER TABLE run_server_gate_overrides ALTER COLUMN gate_type DROP DEFAULT, ALTER COLUMN observed_state DROP DEFAULT;

ALTER TABLE promotion_server_gate_overrides RENAME COLUMN lock_version TO version;
ALTER TABLE promotion_server_gate_overrides
    DROP CONSTRAINT promotion_server_gate_overrides_pkey,
    ADD COLUMN gate_type text NOT NULL DEFAULT 'lock',
    ADD COLUMN observed_state text NOT NULL DEFAULT 'blocking',
    ADD COLUMN window_observation jsonb,
    ADD CONSTRAINT promotion_server_gate_override_shape CHECK (
        (gate_type = 'lock' AND window_observation IS NULL AND observed_state = 'blocking') OR
        (gate_type = 'window' AND window_observation IS NOT NULL AND jsonb_typeof(window_observation) = 'object'
          AND observed_state IN ('blocking','unavailable'))),
    ADD PRIMARY KEY (promotion_id, scope_id, gate_type, version),
    ADD CONSTRAINT promotion_server_gate_override_event_fk FOREIGN KEY (scope_id, gate_type, version)
        REFERENCES server_gate_scope_events (scope_id, gate_type, version);
ALTER TABLE promotion_server_gate_overrides ALTER COLUMN gate_type DROP DEFAULT, ALTER COLUMN observed_state DROP DEFAULT;

CREATE VIEW subject_server_gate_overrides AS
SELECT 'run'::text AS subject_type, run_id AS subject_id, scope_id, gate_type, version, observed_state, window_observation,
    actor_id, actor, reason, recorded_at FROM run_server_gate_overrides
UNION ALL
SELECT 'promotion'::text AS subject_type, promotion_id AS subject_id, scope_id, gate_type, version, observed_state, window_observation,
    actor_id, actor, reason, recorded_at FROM promotion_server_gate_overrides;

-- +goose Down
DROP VIEW subject_server_gate_overrides;

DELETE FROM run_server_gate_overrides WHERE gate_type = 'window';
ALTER TABLE run_server_gate_overrides
    DROP CONSTRAINT run_server_gate_override_event_fk,
    DROP CONSTRAINT run_server_gate_overrides_pkey,
    DROP COLUMN gate_type, DROP COLUMN observed_state, DROP COLUMN window_observation;
ALTER TABLE run_server_gate_overrides RENAME COLUMN version TO lock_version;
ALTER TABLE run_server_gate_overrides ADD PRIMARY KEY (run_id, scope_id, lock_version);

DELETE FROM promotion_server_gate_overrides WHERE gate_type = 'window';
ALTER TABLE promotion_server_gate_overrides
    DROP CONSTRAINT promotion_server_gate_override_event_fk,
    DROP CONSTRAINT promotion_server_gate_overrides_pkey,
    DROP COLUMN gate_type, DROP COLUMN observed_state, DROP COLUMN window_observation;
ALTER TABLE promotion_server_gate_overrides RENAME COLUMN version TO lock_version;
ALTER TABLE promotion_server_gate_overrides ADD PRIMARY KEY (promotion_id, scope_id, lock_version);

DELETE FROM server_gate_scope_events WHERE gate_type = 'window';
ALTER TABLE server_gate_scope_events DROP CONSTRAINT server_gate_scope_events_pkey,
    DROP COLUMN gate_type, DROP COLUMN window_definition, DROP COLUMN window_digest,
    ALTER COLUMN reason SET NOT NULL, ADD CHECK (action IN ('lock','unlock'));
ALTER TABLE server_gate_scope_events RENAME COLUMN version TO lock_version;
ALTER TABLE server_gate_scope_events ADD PRIMARY KEY (scope_id, lock_version);
ALTER TABLE run_server_gate_overrides ADD FOREIGN KEY (scope_id, lock_version)
    REFERENCES server_gate_scope_events (scope_id, lock_version);
ALTER TABLE promotion_server_gate_overrides ADD FOREIGN KEY (scope_id, lock_version)
    REFERENCES server_gate_scope_events (scope_id, lock_version);

CREATE VIEW subject_lock_overrides AS
SELECT 'run'::text AS subject_type, run_id AS subject_id, scope_id, lock_version,
    actor_id, actor, reason, recorded_at FROM run_server_gate_overrides
UNION ALL
SELECT 'promotion'::text AS subject_type, promotion_id AS subject_id, scope_id, lock_version,
    actor_id, actor, reason, recorded_at FROM promotion_server_gate_overrides;

ALTER TABLE server_gate_scopes DROP COLUMN window_definition, DROP COLUMN window_digest,
    DROP COLUMN window_version, DROP COLUMN window_state, DROP COLUMN window_evaluated_at,
    DROP COLUMN window_valid_until, DROP COLUMN window_interval_start, DROP COLUMN window_next_transition;
