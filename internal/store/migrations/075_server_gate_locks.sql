-- +goose Up
CREATE TABLE server_gate_scopes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind text NOT NULL,
    target_id uuid UNIQUE REFERENCES targets (id),
    stage_id uuid UNIQUE REFERENCES promotion_stages (id),
    lock_version bigint NOT NULL DEFAULT 0 CHECK (lock_version >= 0),
    lock_reason text CHECK (length(lock_reason) BETWEEN 1 AND 1024 AND btrim(lock_reason) <> ''),
    lock_actor_id uuid REFERENCES principals (id),
    lock_actor jsonb CHECK (jsonb_typeof(lock_actor) = 'object'),
    locked_at timestamptz,
    lock_expires_at timestamptz,
    CHECK ((kind = 'global' AND target_id IS NULL AND stage_id IS NULL)
        OR (kind = 'target' AND target_id IS NOT NULL AND stage_id IS NULL)
        OR (kind = 'stage' AND target_id IS NULL AND stage_id IS NOT NULL)),
    CHECK (num_nonnulls(lock_reason, lock_actor_id, lock_actor, locked_at) IN (0, 4)),
    CHECK (locked_at IS NULL OR lock_version > 0),
    CHECK (lock_expires_at IS NULL OR (locked_at IS NOT NULL AND lock_expires_at > locked_at))
);
CREATE UNIQUE INDEX server_gate_single_global_scope ON server_gate_scopes (kind) WHERE kind = 'global';

-- +goose StatementBegin
CREATE FUNCTION create_server_gate_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_ARGV[0] = 'target' THEN
        INSERT INTO server_gate_scopes (kind, target_id) VALUES ('target', NEW.id);
    ELSE
        INSERT INTO server_gate_scopes (kind, stage_id) VALUES ('stage', NEW.id);
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER create_target_gate_scope AFTER INSERT ON targets
FOR EACH ROW EXECUTE FUNCTION create_server_gate_scope('target');
CREATE TRIGGER create_stage_gate_scope AFTER INSERT ON promotion_stages
FOR EACH ROW EXECUTE FUNCTION create_server_gate_scope('stage');

INSERT INTO server_gate_scopes (kind) VALUES ('global');
INSERT INTO server_gate_scopes (kind, target_id) SELECT 'target', id FROM targets;
INSERT INTO server_gate_scopes (kind, stage_id) SELECT 'stage', id FROM promotion_stages;

CREATE TABLE server_gate_scope_events (
    scope_id uuid NOT NULL REFERENCES server_gate_scopes (id),
    lock_version bigint NOT NULL CHECK (lock_version > 0),
    action text NOT NULL CHECK (action IN ('lock', 'unlock')),
    actor_id uuid NOT NULL REFERENCES principals (id),
    actor jsonb NOT NULL CHECK (jsonb_typeof(actor) = 'object'),
    reason text NOT NULL CHECK (length(reason) BETWEEN 1 AND 1024 AND btrim(reason) <> ''),
    recorded_at timestamptz NOT NULL,
    expires_at timestamptz CHECK (expires_at IS NULL OR (action = 'lock' AND expires_at > recorded_at)),
    PRIMARY KEY (scope_id, lock_version)
);

ALTER TABLE api_keys
DROP CONSTRAINT api_keys_scopes_valid,
ADD CONSTRAINT api_keys_scopes_valid CHECK (
    scopes <@ ARRAY[
        'slipramp:read',
        'slipramp:runs:write',
        'slipramp:runs:review',
        'slipramp:runs:override-gates',
        'slipramp:promotions:override-gates',
        'slipramp:inbox:write',
        'slipramp:promotions:submit',
        'slipramp:promotions:write',
        'slipramp:gates:manage',
        'slipramp:admin'
    ]::text[]
);

-- +goose Down
ALTER TABLE api_keys
DROP CONSTRAINT api_keys_scopes_valid,
ADD CONSTRAINT api_keys_scopes_valid CHECK (
    scopes <@ ARRAY[
        'slipramp:read',
        'slipramp:runs:write',
        'slipramp:runs:review',
        'slipramp:runs:override-gates',
        'slipramp:promotions:override-gates',
        'slipramp:inbox:write',
        'slipramp:promotions:submit',
        'slipramp:promotions:write',
        'slipramp:admin'
    ]::text[]
);

DROP TRIGGER create_target_gate_scope ON targets;
DROP TRIGGER create_stage_gate_scope ON promotion_stages;
DROP FUNCTION create_server_gate_scope();
DROP TABLE server_gate_scope_events;
DROP TABLE server_gate_scopes;
