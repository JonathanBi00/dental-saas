import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { NativeSelect } from "@/components/ui/native-select";
import type { Location } from "@/lib/locations";
import { ROLE_LABELS, ROLES, canBeLocationRestricted, type Role } from "@/lib/roles";

/** Role picker plus, for roles that can be restricted, the locations they may access. */
export function AccessFields({
  idPrefix,
  role,
  locationIds,
  onRoleChange,
  onLocationIdsChange,
  locations,
  canGrantOwner,
}: {
  idPrefix: string;
  role: Role;
  locationIds: string[];
  onRoleChange: (role: Role) => void;
  onLocationIdsChange: (ids: string[]) => void;
  locations: Location[];
  canGrantOwner: boolean;
}) {
  // Only an owner can make someone an owner; keep the current value visible either way.
  const roles = ROLES.filter((r) => r !== "owner" || canGrantOwner || role === "owner");
  const toggle = (id: string, on: boolean) =>
    onLocationIdsChange(on ? [...locationIds, id] : locationIds.filter((x) => x !== id));

  return (
    <>
      <div className="grid gap-1.5">
        <Label htmlFor={`${idPrefix}-role`}>Role</Label>
        <NativeSelect id={`${idPrefix}-role`} value={role} onChange={(e) => onRoleChange(e.target.value as Role)}>
          {roles.map((r) => (
            <option key={r} value={r}>
              {ROLE_LABELS[r]}
            </option>
          ))}
        </NativeSelect>
      </div>
      {canBeLocationRestricted(role) ? (
        <fieldset className="grid gap-2">
          <legend className="mb-1 text-sm font-medium">Locations</legend>
          <p className="text-sm text-muted-foreground">Leave all unticked for access to every location.</p>
          {locations.map((l) => (
            <div key={l.id} className="flex items-center gap-2">
              <Checkbox
                id={`${idPrefix}-loc-${l.id}`}
                checked={locationIds.includes(l.id)}
                onCheckedChange={(v) => toggle(l.id, v === true)}
              />
              <Label htmlFor={`${idPrefix}-loc-${l.id}`} className="font-normal">
                {l.name}
              </Label>
            </div>
          ))}
        </fieldset>
      ) : (
        <p className="text-sm text-muted-foreground">Owners and admins can access every location.</p>
      )}
    </>
  );
}
