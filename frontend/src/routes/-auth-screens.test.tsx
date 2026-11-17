import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { renderApp } from "@/test/render";
import { problem, stubApi } from "@/test/utils";

const user = userEvent.setup();

describe("sign in", () => {
  it("requires an email and password before calling the API", async () => {
    const fetchMock = stubApi({});
    renderApp("/login");

    await user.click(await screen.findByRole("button", { name: "Sign in" }));

    expect(await screen.findByText("Enter your email address")).toBeInTheDocument();
    expect(screen.getByText("Enter your password")).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("rejects a malformed email", async () => {
    const fetchMock = stubApi({});
    renderApp("/login");

    await user.type(await screen.findByLabelText("Email"), "not-an-email");
    await user.type(screen.getByLabelText("Password"), "whatever");
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    expect(await screen.findByText("Enter a valid email address")).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("shows the server's message for wrong credentials and stays on the page", async () => {
    stubApi({ "POST /auth/login": problem(401, "Incorrect email or password.") });
    const { router } = renderApp("/login");

    await user.type(await screen.findByLabelText("Email"), "owner@demo.local");
    await user.type(screen.getByLabelText("Password"), "wrong-password");
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Incorrect email or password.");
    expect(router.state.location.pathname).toBe("/login");
  });
});

describe("forgot password", () => {
  it("validates the email, then shows the same confirmation whatever the outcome", async () => {
    const fetchMock = stubApi({ "POST /auth/password-reset/request": { status: 202 } });
    renderApp("/forgot-password");

    await user.click(await screen.findByRole("button", { name: "Send reset link" }));
    expect(await screen.findByText("Enter your email address")).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();

    await user.type(screen.getByLabelText("Email"), "someone@clinic.com.au");
    await user.click(screen.getByRole("button", { name: "Send reset link" }));
    expect(await screen.findByRole("heading", { name: "Check your email" })).toBeInTheDocument();
  });
});

describe("reset password", () => {
  it("enforces the minimum length and matching confirmation", async () => {
    const fetchMock = stubApi({});
    renderApp("/reset-password/tok");

    await user.type(await screen.findByLabelText("New password"), "short");
    await user.type(screen.getByLabelText("Confirm new password"), "different");
    await user.click(screen.getByRole("button", { name: "Change password" }));

    expect(await screen.findByText("Use at least 12 characters")).toBeInTheDocument();
    expect(screen.getByText("Passwords don't match")).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("explains an expired link", async () => {
    stubApi({ "POST /auth/password-reset/complete": problem(410, "This password reset link is invalid.") });
    renderApp("/reset-password/tok");

    await user.type(await screen.findByLabelText("New password"), "a-long-enough-password");
    await user.type(screen.getByLabelText("Confirm new password"), "a-long-enough-password");
    await user.click(screen.getByRole("button", { name: "Change password" }));

    expect(await screen.findByRole("heading", { name: "Link expired" })).toBeInTheDocument();
  });
});

describe("accept invitation", () => {
  const preview = {
    organisation_name: "Demo Dental",
    email: "new@clinic.com.au",
    role: "front_desk",
    expires_at: "2030-01-01T00:00:00Z",
    existing_account: false,
  };

  it("requires a name and refuses the email address as the password", async () => {
    const fetchMock = stubApi({ "GET /auth/invitations/tok": { status: 200, body: preview } });
    renderApp("/invite/tok");

    expect(await screen.findByRole("heading", { name: "Join Demo Dental" })).toBeInTheDocument();
    await user.type(screen.getByLabelText("Password"), "NEW@clinic.com.au");
    await user.type(screen.getByLabelText("Confirm password"), "NEW@clinic.com.au");
    await user.click(screen.getByRole("button", { name: "Create account and join" }));

    expect(await screen.findByText("Enter your name")).toBeInTheDocument();
    expect(screen.getByText("Don't use your email address as your password")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledTimes(1); // only the preview lookup
  });

  it("asks only for the password when the account already exists", async () => {
    stubApi({ "GET /auth/invitations/tok": { status: 200, body: { ...preview, existing_account: true } } });
    renderApp("/invite/tok");

    const form = (await screen.findByRole("button", { name: "Join" })).closest("form")!;
    expect(within(form).queryByLabelText("Your name")).not.toBeInTheDocument();
    expect(within(form).getByLabelText("Password")).toBeInTheDocument();
  });

  it("says when the invitation is no longer valid", async () => {
    stubApi({ "GET /auth/invitations/tok": problem(410, "This invitation is no longer valid.") });
    renderApp("/invite/tok");

    expect(await screen.findByRole("heading", { name: "Invitation no longer valid" })).toBeInTheDocument();
  });
});
