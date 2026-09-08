CREATE TABLE IF NOT EXISTS guardrail_templates (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    tenant TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (tenant, name)
);

CREATE TABLE IF NOT EXISTS guardrail_template_snapshots (
    id BIGSERIAL PRIMARY KEY,
    template_id BIGINT NOT NULL REFERENCES guardrail_templates(id) ON DELETE CASCADE,
    version INTEGER NOT NULL CHECK (version > 0),
    definition JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (template_id, version)
);
