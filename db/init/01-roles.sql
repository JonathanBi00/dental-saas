-- Runs once, as the postgres superuser, when the database volume is first created.
-- dental_owner: owns the schema and runs migrations.
-- dental_app:   runtime role. Not an owner and NOBYPASSRLS, so row-level security always applies.
CREATE ROLE dental_owner LOGIN PASSWORD 'dental_owner';
CREATE ROLE dental_app   LOGIN PASSWORD 'dental_app' NOBYPASSRLS;

CREATE DATABASE dental OWNER dental_owner;

\connect dental

REVOKE ALL ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO dental_app;
-- citext must be created by a superuser; do it here so migrations can rely on it.
CREATE EXTENSION IF NOT EXISTS citext;

-- dental_auth: owns the few SECURITY DEFINER functions that must look across tenants
-- before a tenant is known (sign-in, invitation lookup). It cannot log in.
CREATE ROLE dental_auth NOLOGIN BYPASSRLS;
GRANT USAGE, CREATE ON SCHEMA public TO dental_auth;
GRANT dental_auth TO dental_owner; -- lets migrations hand function ownership to it
