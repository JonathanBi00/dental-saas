import type { AnyRouter } from "@tanstack/react-router";

/** Screens reachable without a session. A 401 on these is an answer, not an expired session. */
const PUBLIC_PATH_PREFIXES = ["/login", "/forgot-password", "/reset-password", "/invite"];

export function isPublicPath(pathname: string) {
  return PUBLIC_PATH_PREFIXES.some((p) => pathname === p || pathname.startsWith(`${p}/`));
}

/**
 * Sends the user to sign in, remembering where they were so they can return afterwards.
 * Does nothing on public screens, e.g. a wrong password on /login also comes back as 401.
 */
export function redirectToLogin(router: AnyRouter) {
  const { pathname, href } = router.state.location;
  if (isPublicPath(pathname)) return;
  void router.navigate({ to: "/login", search: { redirect: href }, replace: true });
}

/** Only allow same-app paths as a post-sign-in destination (no open redirects). */
export function safeRedirect(target: unknown): string {
  return typeof target === "string" && target.startsWith("/") && !target.startsWith("//")
    ? target
    : "/";
}
