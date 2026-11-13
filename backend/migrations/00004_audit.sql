-- +goose Up
CREATE TABLE audit_events (
  id            uuid PRIMARY KEY, -- UUIDv7, so id order is time order
  org_id        uuid REFERENCES organisations (id), -- NULL for pre-tenant events
  actor_user_id uuid REFERENCES users (id),
  action        text NOT NULL,
  target_type   text,
  target_id     uuid,
  ip            text NOT NULL DEFAULT '',
  user_agent    text NOT NULL DEFAULT '',
  request_id    text NOT NULL DEFAULT '',
  details       jsonb NOT NULL DEFAULT '{}',
  created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_events_org ON audit_events (org_id, id DESC);

ALTER TABLE audit_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_events FORCE ROW LEVEL SECURITY;
CREATE POLICY audit_events_select ON audit_events FOR SELECT USING (org_id = app_org_id());
CREATE POLICY audit_events_insert ON audit_events FOR INSERT
  WITH CHECK (org_id IS NULL OR org_id = app_org_id());

-- Append-only: no update/delete grants, plus a trigger as a second line of defence.
REVOKE UPDATE, DELETE, TRUNCATE ON audit_events FROM dental_app;

-- +goose StatementBegin
CREATE FUNCTION audit_events_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'audit_events is append-only';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER audit_events_no_update BEFORE UPDATE OR DELETE ON audit_events
  FOR EACH ROW EXECUTE FUNCTION audit_events_immutable();

-- +goose Down
DROP TABLE audit_events;
DROP FUNCTION audit_events_immutable();
