DO $migration$
BEGIN
    CREATE TYPE policy_snapshot_status AS ENUM (
        'draft',
        'validated',
        'compiled',
        'staged',
        'active',
        'superseded',
        'rolled_back'
    );
EXCEPTION
    WHEN duplicate_object THEN NULL;
END
$migration$;

-- Keep reruns safe if the type was created by an earlier compatible schema.
ALTER TYPE policy_snapshot_status ADD VALUE IF NOT EXISTS 'draft';
ALTER TYPE policy_snapshot_status ADD VALUE IF NOT EXISTS 'validated';
ALTER TYPE policy_snapshot_status ADD VALUE IF NOT EXISTS 'compiled';
ALTER TYPE policy_snapshot_status ADD VALUE IF NOT EXISTS 'staged';
ALTER TYPE policy_snapshot_status ADD VALUE IF NOT EXISTS 'active';
ALTER TYPE policy_snapshot_status ADD VALUE IF NOT EXISTS 'superseded';
ALTER TYPE policy_snapshot_status ADD VALUE IF NOT EXISTS 'rolled_back';

CREATE TABLE IF NOT EXISTS policies (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    tenant TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_policies_tenant_name
    ON policies (tenant, name) NULLS NOT DISTINCT;

CREATE TABLE IF NOT EXISTS policy_snapshots (
    id BIGSERIAL PRIMARY KEY,
    policy_id BIGINT NOT NULL REFERENCES policies(id) ON DELETE CASCADE,
    version INTEGER NULL CHECK (version IS NULL OR version > 0),
    status policy_snapshot_status NOT NULL,
    definition JSONB NOT NULL,
    integrity_hash TEXT NOT NULL DEFAULT '',
    compiled_at TIMESTAMPTZ NULL,
    activated_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_policy_snapshots_policy_id
    ON policy_snapshots (policy_id);

CREATE UNIQUE INDEX IF NOT EXISTS uq_policy_snapshots_one_active_per_policy
    ON policy_snapshots (policy_id)
    WHERE status = 'active';

CREATE UNIQUE INDEX IF NOT EXISTS uq_policy_snapshots_policy_version
    ON policy_snapshots (policy_id, version)
    WHERE version IS NOT NULL;

CREATE OR REPLACE FUNCTION prevent_compiled_policy_definition_update()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $function$
BEGIN
    IF OLD.status IN ('compiled', 'staged', 'active', 'superseded', 'rolled_back')
       AND NEW.definition IS DISTINCT FROM OLD.definition THEN
        RAISE EXCEPTION 'compiled policy snapshot definition is immutable'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END
$function$;

DROP TRIGGER IF EXISTS trg_policy_snapshots_definition_immutable ON policy_snapshots;
CREATE TRIGGER trg_policy_snapshots_definition_immutable
    BEFORE UPDATE OF definition ON policy_snapshots
    FOR EACH ROW
    EXECUTE FUNCTION prevent_compiled_policy_definition_update();
