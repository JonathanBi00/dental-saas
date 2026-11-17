import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider, createRouter } from "@tanstack/react-router";

import { redirectToLogin } from "@/lib/auth";
import { createQueryClient } from "@/lib/query";
import { routeTree } from "./routeTree.gen";
import "./index.css";

// Any 401 from the API (expired or revoked session) returns the user to sign in.
const queryClient = createQueryClient({ onUnauthorized: () => redirectToLogin(router) });

const router = createRouter({ routeTree, context: { queryClient }, defaultPreload: "intent" });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
);
