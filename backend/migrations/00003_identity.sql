-- +goose Up

-- Users are global identities; one account can belong to several organisations.
CREATE TABLE users (
  id            uuid PRIMARY KEY,
  email         citext NOT NULL UNIQUE,
  name          text NOT NULL,
  password_hash text NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER users_updated_at BEFORE UPDATE ON users
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE memberships (
  id         uuid PRIMARY KEY,
  org_id     uuid NOT NULL REFERENCES organisations (id),
  user_id    uuid NOT NULL REFERENCES users (id),
  role       text NOT NULL CHECK (role IN ('owner', 'admin', 'practitioner', 'front_desk')),
  status     text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'deactivated')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (org_id, user_id)
);
CREATE INDEX memberships_user ON memberships (user_id);
CREATE TRIGGER memberships_updated_at BEFORE UPDATE ON memberships
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- No rows for a membership = unrestricted (all locations).
CREATE TABLE membership_locations (
  org_id        uuid NOT NULL REFERENCES organisations (id),
  membership_id uuid NOT NULL REFERENCES memberships (id) ON DELETE CASCADE,
  location_id   uuid NOT NULL REFERENCES locations (id),
  PRIMARY KEY (membership_id, location_id)
);

CREATE TABLE invitations (
  id           uuid PRIMARY KEY,
  org_id       uuid NOT NULL REFERENCES organisations (id),
  email        citext NOT NULL,
  role         text NOT NULL CHECK (role IN ('owner', 'admin', 'practitioner', 'front_desk')),
  location_ids uuid[] NOT NULL DEFAULT '{}',
  token_hash   bytea NOT NULL UNIQUE,
  invited_by   uuid REFERENCES users (id),
  expires_at   timestamptz NOT NULL,
  accepted_at  timestamptz,
  revoked_at   timestamptz,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX invitations_org_created ON invitations (org_id, created_at, id);
CREATE TRIGGER invitations_updated_at BEFORE UPDATE ON invitations
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Global (not tenant-owned) auth tables. Tokens are stored only as SHA-256 hashes.
CREATE TABLE sessions (
  id                 uuid PRIMARY KEY,
  token_hash         bytea NOT NULL UNIQUE,
  user_id            uuid NOT NULL REFERENCES users (id),
  active_org_id      uuid REFERENCES organisations (id),
  active_location_id uuid REFERENCES locations (id),
  ip                 text NOT NULL DEFAULT '',
  user_agent         text NOT NULL DEFAULT '',
  created_at         timestamptz NOT NULL DEFAULT now(),
  last_seen_at       timestamptz NOT NULL DEFAULT now(),
  expires_at         timestamptz NOT NULL,
  revoked_at         timestamptz
);
CREATE INDEX sessions_user ON sessions (user_id);

CREATE TABLE login_attempts (
  email             citext PRIMARY KEY,
  failures          int NOT NULL,
  window_started_at timestamptz NOT NULL,
  locked_until      timestamptz
);

CREATE TABLE password_resets (
  id         uuid PRIMARY KEY,
  user_id    uuid NOT NULL REFERENCES users (id),
  token_hash bytea NOT NULL UNIQUE,
  expires_at timestamptz NOT NULL,
  used_at    timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- Row-level security ------------------------------------------------------

ALTER TABLE memberships ENABLE ROW LEVEL SECURITY;
ALTER TABLE memberships FORCE ROW LEVEL SECURITY;
-- A user can always see their own memberships, so they can choose an organisation.
CREATE POLICY memberships_select ON memberships FOR SELECT
  USING (org_id = app_org_id() OR user_id = app_user_id());
CREATE POLICY memberships_insert ON memberships FOR INSERT WITH CHECK (org_id = app_org_id());
CREATE POLICY memberships_update ON memberships FOR UPDATE
  USING (org_id = app_org_id()) WITH CHECK (org_id = app_org_id());
CREATE POLICY memberships_delete ON memberships FOR DELETE USING (org_id = app_org_id());

ALTER TABLE membership_locations ENABLE ROW LEVEL SECURITY;
ALTER TABLE membership_locations FORCE ROW LEVEL SECURITY;
CREATE POLICY membership_locations_tenant ON membership_locations
  USING (org_id = app_org_id()) WITH CHECK (org_id = app_org_id());

ALTER TABLE invitations ENABLE ROW LEVEL SECURITY;
ALTER TABLE invitations FORCE ROW LEVEL SECURITY;
CREATE POLICY invitations_tenant ON invitations
  USING (org_id = app_org_id()) WITH CHECK (org_id = app_org_id());

-- Users: visible to themselves and to members of the active organisation.
ALTER TABLE users ENABLE ROW LEVEL SECURITY;
ALTER TABLE users FORCE ROW LEVEL SECURITY;
CREATE POLICY users_select ON users FOR SELECT USING (
  id = app_user_id()
  OR EXISTS (SELECT 1 FROM memberships m WHERE m.user_id = users.id AND m.org_id = app_org_id())
);
CREATE POLICY users_insert ON users FOR INSERT WITH CHECK (id = app_user_id());
CREATE POLICY users_update ON users FOR UPDATE
  USING (id = app_user_id()) WITH CHECK (id = app_user_id());

-- Organisations: also visible where the current user has a membership.
DROP POLICY organisations_select ON organisations;
CREATE POLICY organisations_select ON organisations FOR SELECT USING (
  id = app_org_id()
  OR EXISTS (SELECT 1 FROM memberships m WHERE m.org_id = organisations.id AND m.user_id = app_user_id())
);

-- Pre-tenant lookups ------------------------------------------------------
-- These are the ONLY cross-tenant read paths for dental_app. They are owned by
-- dental_auth (BYPASSRLS), do exact-key lookups only, and pin search_path.
GRANT SELECT ON users, invitations, organisations TO dental_auth;

-- +goose StatementBegin
CREATE FUNCTION auth_find_user_by_email(p_email citext)
RETURNS TABLE (id uuid, email citext, name text, password_hash text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp AS $$
  SELECT u.id, u.email, u.name, u.password_hash FROM users u WHERE u.email = p_email
$$;
-- +goose StatementEnd
ALTER FUNCTION auth_find_user_by_email(citext) OWNER TO dental_auth;
REVOKE ALL ON FUNCTION auth_find_user_by_email(citext) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION auth_find_user_by_email(citext) TO dental_app;

-- +goose StatementBegin
CREATE FUNCTION auth_find_invitation(p_token_hash bytea)
RETURNS TABLE (
  id uuid, org_id uuid, org_name text, org_status text, email citext, role text,
  location_ids uuid[], expires_at timestamptz, accepted_at timestamptz, revoked_at timestamptz
)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp AS $$
  SELECT i.id, i.org_id, o.name, o.status, i.email, i.role,
         i.location_ids, i.expires_at, i.accepted_at, i.revoked_at
  FROM invitations i JOIN organisations o ON o.id = i.org_id
  WHERE i.token_hash = p_token_hash
$$;
-- +goose StatementEnd
ALTER FUNCTION auth_find_invitation(bytea) OWNER TO dental_auth;
REVOKE ALL ON FUNCTION auth_find_invitation(bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION auth_find_invitation(bytea) TO dental_app;

-- +goose Down
DROP FUNCTION auth_find_invitation(bytea);
DROP FUNCTION auth_find_user_by_email(citext);
REVOKE SELECT ON users, invitations, organisations FROM dental_auth;
DROP POLICY organisations_select ON organisations;
CREATE POLICY organisations_select ON organisations FOR SELECT USING (id = app_org_id());
DROP POLICY users_select ON users; -- references memberships
DROP TABLE password_resets;
DROP TABLE login_attempts;
DROP TABLE sessions;
DROP TABLE invitations;
DROP TABLE membership_locations;
DROP TABLE memberships;
DROP TABLE users;
