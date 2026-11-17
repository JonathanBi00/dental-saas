import { queryOptions } from "@tanstack/react-query";

import { api, unwrap, type Schemas } from "@/api/client";

export type Location = Schemas["Location"];

/**
 * Locations the member can access. MVP: one page of up to 200, which covers any single
 * practice group for now; paging through `next_cursor` is a follow-up.
 */
export const locationsQuery = (includeArchived = false) =>
  queryOptions({
    queryKey: ["locations", { includeArchived }],
    queryFn: () =>
      unwrap(api.GET("/locations", { params: { query: { limit: 200, include_archived: includeArchived } } })),
    select: (page) => page.items,
  });
