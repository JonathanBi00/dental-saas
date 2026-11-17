import type { Schemas } from "@/api/client";

export type Role = Schemas["Role"];

export const ROLES: readonly Role[] = ["owner", "admin", "practitioner", "front_desk"];

export const ROLE_LABELS: Record<Role, string> = {
  owner: "Owner",
  admin: "Admin",
  practitioner: "Practitioner",
  front_desk: "Front desk",
};

/** Owners and admins always see every location, so restrictions only apply to the other roles. */
export function canBeLocationRestricted(role: Role) {
  return role === "practitioner" || role === "front_desk";
}
