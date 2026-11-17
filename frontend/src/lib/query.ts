import { MutationCache, QueryCache, QueryClient } from "@tanstack/react-query";

import { isApiError } from "@/api/client";

export interface QueryClientOptions {
  /** Called whenever a query or mutation fails with 401 (no session, or it expired). */
  onUnauthorized: () => void;
}

export function createQueryClient({ onUnauthorized }: QueryClientOptions) {
  const handleError = (err: unknown) => {
    if (isApiError(err, 401)) onUnauthorized();
  };

  return new QueryClient({
    queryCache: new QueryCache({ onError: handleError }),
    mutationCache: new MutationCache({ onError: handleError }),
    defaultOptions: {
      queries: {
        staleTime: 30_000,
        refetchOnWindowFocus: false,
        // A 4xx won't fix itself on retry; network errors and 5xx get one more try.
        retry: (failureCount, err) => !(isApiError(err) && err.status < 500) && failureCount < 1,
      },
    },
  });
}
