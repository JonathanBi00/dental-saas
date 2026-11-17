import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";

import { api, unwrap } from "@/api/client";
import { sessionQuery, type Session } from "@/lib/session";

const notSession = { predicate: (q: { queryKey: readonly unknown[] }) => q.queryKey[0] !== "session" };

/** Switch the active organisation. Everything cached belongs to the old one, so it's reset. */
export function useSwitchOrganisation() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  return useMutation({
    mutationFn: (organisationId: string) =>
      unwrap(api.POST("/auth/session/organisation", { body: { organisation_id: organisationId } })),
    onSuccess: async (session: Session) => {
      queryClient.setQueryData(sessionQuery.queryKey, session);
      await navigate({ to: "/" });
      await queryClient.resetQueries(notSession);
    },
  });
}

/** Switch the active location (null = all accessible locations). */
export function useSwitchLocation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (locationId: string | null) =>
      unwrap(api.POST("/auth/session/location", { body: { location_id: locationId } })),
    onSuccess: async (session: Session) => {
      queryClient.setQueryData(sessionQuery.queryKey, session);
      // Location-scoped data (none yet beyond locations) is refetched for the new location.
      await queryClient.invalidateQueries(notSession);
    },
  });
}

export function useSignOut() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  return useMutation({
    mutationFn: () => unwrap(api.POST("/auth/logout")),
    onSettled: async () => {
      // Even if the call failed (e.g. session already expired), leave no data behind.
      await navigate({ to: "/login", replace: true });
      queryClient.clear();
    },
  });
}
