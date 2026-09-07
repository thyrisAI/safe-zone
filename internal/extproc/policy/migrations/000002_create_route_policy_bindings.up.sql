CREATE TABLE IF NOT EXISTS route_policy_bindings (
    id BIGSERIAL PRIMARY KEY,
    gateway_name TEXT NOT NULL,
    listener_name TEXT,
    route_name TEXT,
    rule_name TEXT,
    policy_name TEXT NOT NULL,
    tenant TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (gateway_name, listener_name, route_name, rule_name)
);
