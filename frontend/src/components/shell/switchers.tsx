import { useQuery } from "@tanstack/react-query";

import { NativeSelect } from "@/components/ui/native-select";
import { locationsQuery } from "@/lib/locations";
import type { Session } from "@/lib/session";
import { useSwitchLocation, useSwitchOrganisation } from "./use-switch-session";

function usableMemberships(session: Session) {
  return session.memberships.filter((m) => m.status === "active" && m.organisation_status === "active");
}

export function OrganisationSwitcher({ session }: { session: Session }) {
  const switchOrg = useSwitchOrganisation();
  const orgs = usableMemberships(session);
  const active = session.memberships.find((m) => m.organisation_id === session.active_organisation_id);

  if (orgs.length <= 1) {
    return <span className="truncate font-medium">{active?.organisation_name}</span>;
  }
  return (
    <NativeSelect
      aria-label="Organisation"
      className="w-auto max-w-56"
      value={session.active_organisation_id ?? ""}
      disabled={switchOrg.isPending}
      onChange={(e) => switchOrg.mutate(e.target.value)}
    >
      {orgs.map((m) => (
        <option key={m.organisation_id} value={m.organisation_id}>
          {m.organisation_name}
        </option>
      ))}
    </NativeSelect>
  );
}

const ALL = "all";

export function LocationSwitcher({ session }: { session: Session }) {
  const switchLocation = useSwitchLocation();
  const locations = useQuery(locationsQuery());

  if (!locations.data) return null;
  return (
    <NativeSelect
      aria-label="Location"
      className="w-auto max-w-56"
      value={session.active_location_id ?? ALL}
      disabled={switchLocation.isPending}
      onChange={(e) => switchLocation.mutate(e.target.value === ALL ? null : e.target.value)}
    >
      {locations.data.length > 1 && <option value={ALL}>All locations</option>}
      {locations.data.map((l) => (
        <option key={l.id} value={l.id}>
          {l.name}
        </option>
      ))}
    </NativeSelect>
  );
}

/** Shown when a member of several organisations hasn't picked one yet. */
export function OrganisationPicker({ session }: { session: Session }) {
  const switchOrg = useSwitchOrganisation();
  const orgs = usableMemberships(session);

  return (
    <div className="mx-auto grid max-w-sm gap-4 py-12">
      <h1 className="text-xl font-semibold">Choose a practice</h1>
      {orgs.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          Your account doesn't have access to any active practice. Ask a practice owner to invite you.
        </p>
      ) : (
        <ul className="grid gap-2">
          {orgs.map((m) => (
            <li key={m.organisation_id}>
              <button
                type="button"
                disabled={switchOrg.isPending}
                onClick={() => switchOrg.mutate(m.organisation_id)}
                className="w-full rounded-md border px-4 py-3 text-left hover:bg-accent disabled:opacity-50"
              >
                {m.organisation_name}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
