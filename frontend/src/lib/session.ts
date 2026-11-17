import { queryOptions, useSuspenseQuery } from "@tanstack/react-query";

import { api, unwrap, type Schemas } from "@/api/client";

export type Session = Schemas["SessionInfo"];

export const sessionQuery = queryOptions({
  queryKey: ["session"],
  queryFn: () => unwrap(api.GET("/auth/session")),
  staleTime: 60_000,
});

/** The signed-in session. Only use under the authenticated layout, which loads it first. */
export function useSession() {
  return useSuspenseQuery(sessionQuery).data;
}

export function hasPermission(session: Pick<Session, "permissions">, permission: string) {
  return session.permissions.includes(permission);
}
