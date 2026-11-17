import { Outlet, createFileRoute, redirect } from "@tanstack/react-router";
import { LogOut } from "lucide-react";

import { isApiError } from "@/api/client";
import { AppNav } from "@/components/shell/app-nav";
import { LocationSwitcher, OrganisationPicker, OrganisationSwitcher } from "@/components/shell/switchers";
import { useSignOut } from "@/components/shell/use-switch-session";
import { Button } from "@/components/ui/button";
import { sessionQuery, useSession } from "@/lib/session";

/** Layout for every signed-in screen. Loads the session first; no session means sign in. */
export const Route = createFileRoute("/_app")({
  beforeLoad: async ({ context, location }) => {
    try {
      await context.queryClient.ensureQueryData(sessionQuery);
    } catch (err) {
      if (isApiError(err, 401)) throw redirect({ to: "/login", search: { redirect: location.href }, replace: true });
      throw err;
    }
  },
  component: AppLayout,
});

function AppLayout() {
  const session = useSession();
  const signOut = useSignOut();
  const hasOrg = session.active_organisation_id !== null;

  return (
    <div className="flex min-h-svh flex-col bg-background text-foreground">
      <header className="flex flex-wrap items-center gap-x-4 gap-y-2 border-b px-4 py-2">
        <span className="font-semibold tracking-tight">Dental SaaS</span>
        {hasOrg && (
          <div className="flex min-w-0 flex-wrap items-center gap-2 text-sm">
            <OrganisationSwitcher session={session} />
            <LocationSwitcher session={session} />
          </div>
        )}
        <div className="ml-auto flex items-center gap-2 text-sm">
          <span className="hidden text-muted-foreground sm:inline">{session.user.name}</span>
          <Button variant="ghost" size="sm" onClick={() => signOut.mutate()} disabled={signOut.isPending}>
            <LogOut aria-hidden />
            Sign out
          </Button>
        </div>
      </header>
      {hasOrg ? (
        <div className="flex flex-1 flex-col md:flex-row">
          <aside className="border-b p-3 md:w-56 md:shrink-0 md:border-r md:border-b-0">
            <AppNav session={session} />
          </aside>
          <main className="min-w-0 flex-1 p-4 md:p-6">
            <Outlet />
          </main>
        </div>
      ) : (
        <main className="flex-1 p-4">
          <OrganisationPicker session={session} />
        </main>
      )}
    </div>
  );
}
