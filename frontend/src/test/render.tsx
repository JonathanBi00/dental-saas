import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider, createMemoryHistory, createRouter } from "@tanstack/react-router";
import { render } from "@testing-library/react";

import { redirectToLogin } from "@/lib/auth";
import { createQueryClient } from "@/lib/query";
import { routeTree } from "@/routeTree.gen";

/** Renders the real app (all routes, real Query setup) at `path`. Stub the API with `stubApi`. */
export function renderApp(path: string) {
  const queryClient = createQueryClient({ onUnauthorized: () => redirectToLogin(router) });
  const router = createRouter({
    routeTree,
    context: { queryClient },
    history: createMemoryHistory({ initialEntries: [path] }),
  });
  const result = render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return { ...result, router, queryClient };
}
