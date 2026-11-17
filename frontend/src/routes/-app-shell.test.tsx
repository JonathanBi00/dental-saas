import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { frontDeskSession, ownerSession, page, parramatta, PARRAMATTA_ID, sydney } from "@/test/fixtures";
import { renderApp } from "@/test/render";
import { problem, stubApi } from "@/test/utils";

const user = userEvent.setup();

describe("app shell", () => {
  it("shows the settings navigation to an owner", async () => {
    stubApi({
      "GET /auth/session": { status: 200, body: ownerSession },
      "GET /locations": page([sydney, parramatta]),
    });
    renderApp("/");

    const nav = await screen.findByRole("navigation", { name: "Main" });
    expect(within(nav).getByRole("link", { name: "Locations" })).toBeInTheDocument();
    expect(within(nav).getByRole("link", { name: "Users" })).toBeInTheDocument();
    expect(within(nav).getByRole("link", { name: "Audit log" })).toBeInTheDocument();
  });

  it("hides admin navigation from front desk", async () => {
    stubApi({
      "GET /auth/session": { status: 200, body: frontDeskSession },
      "GET /locations": page([parramatta]),
    });
    renderApp("/");

    const nav = await screen.findByRole("navigation", { name: "Main" });
    expect(within(nav).getByRole("link", { name: "Home" })).toBeInTheDocument();
    expect(within(nav).queryByText("Settings")).not.toBeInTheDocument();
    expect(within(nav).queryByRole("link", { name: "Locations" })).not.toBeInTheDocument();
    expect(within(nav).queryByRole("link", { name: "Users" })).not.toBeInTheDocument();
    expect(within(nav).queryByRole("link", { name: "Audit log" })).not.toBeInTheDocument();
  });

  it("blocks an admin screen opened directly by front desk", async () => {
    stubApi({
      "GET /auth/session": { status: 200, body: frontDeskSession },
      "GET /locations": page([parramatta]),
    });
    renderApp("/settings/users");

    expect(await screen.findByRole("heading", { name: "Not available" })).toBeInTheDocument();
  });

  it("sends a signed-out visitor to sign in, then back", async () => {
    stubApi({ "GET /auth/session": problem(401, "Unauthorized") });
    const { router } = renderApp("/settings/locations");

    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
    expect(router.state.location.search).toEqual({ redirect: "/settings/locations" });
  });

  it("switches the active location", async () => {
    const fetchMock = stubApi({
      "GET /auth/session": { status: 200, body: ownerSession },
      "GET /locations": page([sydney, parramatta]),
      "POST /auth/session/location": async (req) => {
        const body = (await req.json()) as { location_id: string | null };
        return { status: 200, body: { ...ownerSession, active_location_id: body.location_id } };
      },
    });
    renderApp("/");

    const select = await screen.findByRole("combobox", { name: "Location" });
    expect(select).toHaveValue("all");
    await user.selectOptions(select, "Parramatta");

    await expect.poll(() => (select as HTMLSelectElement).value).toBe(PARRAMATTA_ID);
    const call = fetchMock.mock.calls.find(([r]) => r.method === "POST");
    expect(call?.[0].url).toMatch(/\/auth\/session\/location$/);
  });
});
