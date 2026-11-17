import { createFileRoute } from "@tanstack/react-router";

import { ROLE_LABELS } from "@/lib/roles";
import { useSession } from "@/lib/session";

export const Route = createFileRoute("/_app/")({
  component: HomePage,
});

function HomePage() {
  const session = useSession();
  const membership = session.memberships.find((m) => m.organisation_id === session.active_organisation_id);

  return (
    <div className="grid max-w-2xl gap-2">
      <h1 className="text-2xl font-semibold tracking-tight">Welcome, {session.user.name}</h1>
      <p className="text-muted-foreground">
        You're signed in to {membership?.organisation_name}
        {session.role && <> as {ROLE_LABELS[session.role]}</>}. Patients, appointments and the rest of the
        practice features arrive in later releases.
      </p>
    </div>
  );
}
