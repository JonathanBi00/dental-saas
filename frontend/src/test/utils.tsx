import { vi } from "vitest";

type Route = { status: number; body?: unknown };

/**
 * Stubs `fetch` with canned responses keyed by "METHOD /path" (path relative to /api/v1).
 * Unmatched requests fail the test loudly with a 599.
 */
export function stubApi(routes: Record<string, Route | ((req: Request) => Route | Promise<Route>)>) {
  const fetchMock = vi.fn(async (input: Request) => {
    const url = new URL(input.url);
    const key = `${input.method} ${url.pathname.replace(/^\/api\/v1/, "")}`;
    const handler = routes[key];
    if (!handler) return json(599, { title: `No stub for ${key}`, status: 599 });
    const { status, body } = typeof handler === "function" ? await handler(input) : handler;
    return json(status, body);
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function json(status: number, body: unknown) {
  if (body === undefined || status === 204) return new Response(null, { status });
  const type = status >= 400 ? "application/problem+json" : "application/json";
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": type } });
}

export function problem(status: number, title: string, errors?: { field: string; message: string }[]) {
  return { status, body: { type: "about:blank", title, status, request_id: "test", errors } };
}
