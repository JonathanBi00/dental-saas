import type { FieldValues, Path, UseFormSetError } from "react-hook-form";
import { z } from "zod";

import { isApiError } from "@/api/client";

// Mirrors the server's password policy (identity/password.go) so users find out before submitting.
export const MIN_PASSWORD_LENGTH = 12;
const MAX_PASSWORD_LENGTH = 256;

export const emailSchema = z
  .string()
  .trim()
  .min(1, "Enter your email address")
  .pipe(z.email("Enter a valid email address"));

export const newPasswordSchema = z
  .string()
  .min(MIN_PASSWORD_LENGTH, `Use at least ${MIN_PASSWORD_LENGTH} characters`)
  .max(MAX_PASSWORD_LENGTH, `Use at most ${MAX_PASSWORD_LENGTH} characters`);

/**
 * A form with a new password + confirmation, plus any `extra` fields.
 * `email`, when known, may not be used as the password.
 */
export function newPasswordForm<S extends z.ZodRawShape>(extra: S, email?: string) {
  // TypeScript can't see `password`/`confirm` through the generic spread, hence the cast.
  type Passwords = { password: string; confirm: string };
  return z
    .object({ ...extra, password: newPasswordSchema, confirm: z.string() })
    .refine(
      (v) => !email || (v as Passwords).password.trim().toLowerCase() !== email.trim().toLowerCase(),
      { path: ["password"], message: "Don't use your email address as your password" },
    )
    .refine((v) => (v as Passwords).password === (v as Passwords).confirm, {
      path: ["confirm"],
      message: "Passwords don't match",
    });
}

/**
 * Puts an API error onto a react-hook-form form: 422 field errors go under their fields
 * (when the form has that field), anything else becomes `errors.root.server`.
 */
export function setServerErrors<T extends FieldValues>(
  err: unknown,
  setError: UseFormSetError<T>,
  fields: readonly Path<T>[],
) {
  if (!isApiError(err)) {
    setError("root.server", { message: "Couldn't reach the server. Check your connection and try again." });
    return;
  }
  let placed = false;
  for (const [field, message] of Object.entries(err.fieldErrors)) {
    if ((fields as readonly string[]).includes(field)) {
      setError(field as Path<T>, { message: `${humanize(field)} ${message}` });
      placed = true;
    }
  }
  if (!placed) setError("root.server", { message: err.message });
}

function humanize(field: string) {
  const words = field.replace(/_ids?$/, "").replace(/_/g, " ");
  return words.charAt(0).toUpperCase() + words.slice(1);
}
