import { QueryClientProvider, useMutation, useQuery } from "@tanstack/react-query";
import {
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { api, unwrap } from "@/api/client";
import { redirectToLogin } from "@/lib/auth";
import { createQueryClient } from "@/lib/query";
import { problem, stubApi } from "@/test/utils";

function setup(initialPath: string) {
  const root = createRootRoute();
  const login = createRoute({
    getParentRoute: () => root,
    path: "/login",
    component: function Login() {
      const signIn = useMutation({
        mutationFn: () => unwrap(api.POST("/auth/login", { body: { email: "a@b.c", password: "x" } })),
      });
      return (
        <>
          <h1>Sign in</h1>
          <button onClick={() => signIn.mutate()}>Submit</button>
          {signIn.isError && <p>Wrong email or password</p>}
        </>
      );
    },
  });
  const locations = createRoute({
    getParentRoute: () => root,
    path: "/settings/locations",
    component: function Locations() {
      const q = useQuery({ queryKey: ["locations"], queryFn: () => unwrap(api.GET("/locations")) });
      return <h1>{q.isSuccess ? `${q.data.items.length} locations` : "Loading"}</h1>;
    },
  });

  const queryClient = createQueryClient({ onUnauthorized: () => redirectToLogin(router) });
  const router = createRouter({
    routeTree: root.addChildren([login, locations]),
    history: createMemoryHistory({ initialEntries: [initialPath] }),
  });
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return { router };
}

describe("401 handling", () => {
  it("sends the user to /login, remembering where they were", async () => {
    stubApi({ "GET /locations": problem(401, "Unauthorized") });
    const { router } = setup("/settings/locations");

    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/login");
    expect(router.state.location.search).toEqual({ redirect: "/settings/locations" });
  });

  it("stays on the page for successful responses", async () => {
    stubApi({ "GET /locations": { status: 200, body: { items: [], next_cursor: null } } });
    const { router } = setup("/settings/locations");

    expect(await screen.findByRole("heading", { name: "0 locations" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/settings/locations");
  });

  it("does not redirect for a 401 on a public screen (wrong password)", async () => {
    const fetchMock = stubApi({ "POST /auth/login": problem(401, "Invalid email or password") });
    const { router } = setup("/login");

    await userEvent.click(await screen.findByRole("button", { name: "Submit" }));

    expect(await screen.findByText("Wrong email or password")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(router.state.location.search).toEqual({});
  });
});
