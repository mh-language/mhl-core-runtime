-- Seed data for the mhl-sql-postgres smoke test and CENARIO scenarios.
CREATE TABLE people (
    id         serial PRIMARY KEY,
    name       text NOT NULL,
    org        text NOT NULL,
    active     boolean NOT NULL DEFAULT true,
    score      numeric(6,2),
    created_at timestamptz NOT NULL DEFAULT '2026-01-02T03:04:05Z',
    tags       jsonb NOT NULL DEFAULT '[]'
);

INSERT INTO people (name, org, active, score, tags) VALUES
    ('Ana',   'acme',   true,  91.50, '["lead","eu"]'),
    ('Bruno', 'acme',   true,  70.00, '["eu"]'),
    ('Cara',  'acme',   false, 55.25, '[]'),
    ('Davi',  'globex', true,  88.00, '["lead"]'),
    ('Eva',   'globex', true,  63.75, '["us"]');
