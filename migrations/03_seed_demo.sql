-- Demo users for local testing and defense demo.
-- JWT secret for dev: ridehail_dev_secret (or set JWT_SECRET env)

BEGIN;

INSERT INTO users (id, email, role, status, password_hash)
VALUES
    ('11111111-1111-4111-8111-111111111111', 'passenger@example.com', 'PASSENGER', 'ACTIVE', 'demo_hash'),
    ('22222222-2222-4222-8222-222222222222', 'driver@example.com', 'DRIVER', 'ACTIVE', 'demo_hash'),
    ('33333333-3333-4333-8333-333333333333', 'admin@example.com', 'ADMIN', 'ACTIVE', 'demo_hash')
ON CONFLICT (id) DO NOTHING;

COMMIT;
