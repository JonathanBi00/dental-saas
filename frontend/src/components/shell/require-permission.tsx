import type { ReactNode } from "react";

import { hasPermission, useSession } from "@/lib/session";

/**
 * Hides a screen from members without the permission (e.g. a front desk user typing the URL).
 * The API enforces the same rule; this only avoids showing a broken page.
 */
export function RequirePermission({ permission, children }: { permission: string; children: ReactNode }) {
  const session = useSession();
  if (!hasPermission(session, permission)) {
    return (
      <div className="grid max-w-xl gap-2">
        <h1 className="text-xl font-semibold">Not available</h1>
        <p className="text-muted-foreground">Your role doesn't have access to this page.</p>
      </div>
    );
  }
  return children;
}
