-- Tracks live CRD ownership of controller-generated Inline policies without
-- deleting immutable policy snapshots when an attachment is removed.
CREATE TABLE IF NOT EXISTS owner_crd_refs (
    id BIGSERIAL PRIMARY KEY,
    policy_name TEXT NOT NULL,
    tenant TEXT,
    owner_namespace TEXT NOT NULL,
    owner_name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (policy_name, tenant, owner_namespace, owner_name)
);
