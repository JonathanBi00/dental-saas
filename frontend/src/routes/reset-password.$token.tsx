import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation } from "@tanstack/react-query";
import { Link, createFileRoute } from "@tanstack/react-router";
import { useForm } from "react-hook-form";
import type { z } from "zod";

import { api, isApiError, unwrap } from "@/api/client";
import { AuthLayout } from "@/components/auth-layout";
import { FormField } from "@/components/form-field";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { MIN_PASSWORD_LENGTH, newPasswordForm, setServerErrors } from "@/lib/forms";

export const Route = createFileRoute("/reset-password/$token")({
  component: ResetPasswordPage,
});

const schema = newPasswordForm({});
type Values = z.infer<typeof schema>;

function ResetPasswordPage() {
  const { token } = Route.useParams();
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: { password: "", confirm: "" } });
  const { errors } = form.formState;

  const complete = useMutation({
    mutationFn: ({ password }: Values) =>
      unwrap(api.POST("/auth/password-reset/complete", { body: { token, password } })),
    onError: (err) => {
      if (!isApiError(err, 410)) setServerErrors(err, form.setError, ["password"]);
    },
  });

  if (complete.isSuccess) {
    return (
      <AuthLayout title="Password changed">
        <div className="grid gap-4 text-sm">
          <p>Your password has been changed and you've been signed out everywhere else.</p>
          <Button asChild>
            <Link to="/login">Sign in</Link>
          </Button>
        </div>
      </AuthLayout>
    );
  }

  if (isApiError(complete.error, 410)) {
    return (
      <AuthLayout title="Link expired">
        <div className="grid gap-4 text-sm">
          <p>This reset link is invalid, has expired or has already been used.</p>
          <Button asChild>
            <Link to="/forgot-password">Request a new link</Link>
          </Button>
        </div>
      </AuthLayout>
    );
  }

  return (
    <AuthLayout title="Choose a new password">
      <form className="grid gap-4" noValidate onSubmit={form.handleSubmit((v) => complete.mutate(v))}>
        {errors.root?.server && (
          <Alert variant="destructive">
            <AlertDescription>{errors.root.server.message}</AlertDescription>
          </Alert>
        )}
        <FormField
          label="New password"
          type="password"
          autoComplete="new-password"
          autoFocus
          hint={`At least ${MIN_PASSWORD_LENGTH} characters.`}
          error={errors.password?.message}
          {...form.register("password")}
        />
        <FormField
          label="Confirm new password"
          type="password"
          autoComplete="new-password"
          error={errors.confirm?.message}
          {...form.register("confirm")}
        />
        <Button type="submit" disabled={complete.isPending}>
          {complete.isPending ? "Saving…" : "Change password"}
        </Button>
      </form>
    </AuthLayout>
  );
}
