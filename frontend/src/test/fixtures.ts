import type { Schemas } from "@/api/client";
import type { Session } from "@/lib/session";

export const ORG_ID = "0190a000-0000-7000-8000-000000000001";
export const SYDNEY_ID = "0190a000-0000-7000-8000-0000000000a1";
export const PARRAMATTA_ID = "0190a000-0000-7000-8000-0000000000a2";

const membership = {
  organisation_id: ORG_ID,
  organisation_name: "Demo Dental",
  organisation_status: "active",
  status: "active",
} as const;

export const ownerSession: Session = {
  user: { id: "0190a000-0000-7000-8000-0000000000u1", email: "owner@demo.local", name: "Olivia Owner" },
  memberships: [{ ...membership, role: "owner" }],
  active_organisation_id: ORG_ID,
  active_location_id: null,
  role: "owner",
  permissions: ["locations:read", "locations:manage", "members:manage", "audit:read"],
  accessible_location_ids: [SYDNEY_ID, PARRAMATTA_ID],
};

export const frontDeskSession: Session = {
  user: { id: "0190a000-0000-7000-8000-0000000000u2", email: "frontdesk@demo.local", name: "Frank Desk" },
  memberships: [{ ...membership, role: "front_desk" }],
  active_organisation_id: ORG_ID,
  active_location_id: null,
  role: "front_desk",
  permissions: ["locations:read"],
  accessible_location_ids: [PARRAMATTA_ID],
};

export function location(id: string, name: string, extra: Partial<Schemas["Location"]> = {}): Schemas["Location"] {
  return {
    id,
    name,
    address_line1: "",
    address_line2: "",
    suburb: "",
    state: "NSW",
    postcode: "",
    phone: "",
    email: "",
    timezone: "Australia/Sydney",
    archived_at: null,
    created_at: "2026-11-01T00:00:00Z",
    updated_at: "2026-11-01T00:00:00Z",
    ...extra,
  };
}

export const sydney = location(SYDNEY_ID, "Sydney CBD");
export const parramatta = location(PARRAMATTA_ID, "Parramatta");

export const page = <T>(items: T[]) => ({ status: 200, body: { items, next_cursor: null } });
