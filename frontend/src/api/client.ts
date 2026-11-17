import createClient from "openapi-fetch";

import type { components, paths } from "./schema.gen";

export type Schemas = components["schemas"];
export type Problem = Schemas["Problem"];

/**
 * Typed client generated from api/openapi.yaml (run `task gen` after changing the spec).
 *
 * The base URL is absolute so the client also works under jsdom in tests. In the browser it
 * is same-origin: Vite forwards /api to the Go API, so the session cookie is sent automatically.
 * `fetch` is resolved on every call so tests can stub `globalThis.fetch`.
 */
export const api = createClient<paths>({
  baseUrl: new URL("/api/v1", window.location.origin).href,
  fetch: (request) => globalThis.fetch(request),
});

/** An API error response, carrying the problem+json body when there is one. */
export class ApiError extends Error {
  readonly status: number;
  readonly problem?: Problem;

  constructor(status: number, problem?: Problem) {
    super(problem?.detail ?? problem?.title ?? `Request failed with status ${status}`);
    this.name = "ApiError";
    this.status = status;
    this.problem = problem;
  }

  /** Field-level messages from a 422 response, keyed by field name. */
  get fieldErrors(): Record<string, string> {
    return Object.fromEntries((this.problem?.errors ?? []).map((e) => [e.field, e.message]));
  }
}

export function isApiError(err: unknown, status?: number): err is ApiError {
  return err instanceof ApiError && (status === undefined || err.status === status);
}

/**
 * Turns an openapi-fetch result into its data, or throws an ApiError, so that TanStack Query
 * sees failures as errors: `queryFn: () => unwrap(api.GET("/locations"))`.
 */
export async function unwrap<T>(
  result: Promise<{ data?: T; error?: unknown; response: Response }>,
): Promise<T> {
  const { data, error, response } = await result;
  if (!response.ok) {
    throw new ApiError(response.status, isProblem(error) ? error : undefined);
  }
  return data as T;
}

function isProblem(value: unknown): value is Problem {
  return typeof value === "object" && value !== null && "status" in value && "title" in value;
}
