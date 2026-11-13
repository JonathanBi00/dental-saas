-- +goose Up
CREATE TABLE organisations (
  id         uuid PRIMARY KEY,
  name       text NOT NULL CHECK (length(btrim(name)) > 0),
  abn        text,
  status     text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER organisations_updated_at BEFORE UPDATE ON organisations
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE organisations ENABLE ROW LEVEL SECURITY;
ALTER TABLE organisations FORCE ROW LEVEL SECURITY;
-- Replaced in 00003 once memberships exist.
CREATE POLICY organisations_select ON organisations FOR SELECT USING (id = app_org_id());
CREATE POLICY organisations_insert ON organisations FOR INSERT WITH CHECK (id = app_org_id());
CREATE POLICY organisations_update ON organisations FOR UPDATE
  USING (id = app_org_id()) WITH CHECK (id = app_org_id());

CREATE TABLE locations (
  id            uuid PRIMARY KEY,
  org_id        uuid NOT NULL REFERENCES organisations (id),
  name          text NOT NULL CHECK (length(btrim(name)) > 0),
  address_line1 text NOT NULL DEFAULT '',
  address_line2 text NOT NULL DEFAULT '',
  suburb        text NOT NULL DEFAULT '',
  state         text NOT NULL DEFAULT '',
  postcode      text NOT NULL DEFAULT '',
  phone         text NOT NULL DEFAULT '',
  email         text NOT NULL DEFAULT '',
  timezone      text NOT NULL,
  archived_at   timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX locations_org_name ON locations (org_id, name, id);
CREATE INDEX locations_org_updated ON locations (org_id, updated_at, id);
CREATE TRIGGER locations_updated_at BEFORE UPDATE ON locations
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE locations ENABLE ROW LEVEL SECURITY;
ALTER TABLE locations FORCE ROW LEVEL SECURITY;
CREATE POLICY locations_tenant ON locations
  USING (org_id = app_org_id()) WITH CHECK (org_id = app_org_id());

-- +goose Down
DROP TABLE locations;
DROP TABLE organisations;
