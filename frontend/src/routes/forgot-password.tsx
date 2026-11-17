import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation } from "@tanstack/react-query";
import { Link, createFileRoute } from "@tanstack/react-router";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { api, unwrap } from "@/api/client";
import { AuthLayout } from "@/components/auth-layout";
import { FormField } from "@/components/form-field";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { emailSchema, setServerErrors } from "@/lib/forms";

export const Route = createFileRoute("/forgot-password")({
  component: ForgotPasswordPage,
});

const schema = z.object({ email: emailSchema });
type Values = z.infer<typeof schema>;

function ForgotPasswordPage() {
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: { email: "" } });
  const { errors } = form.formState;

  const request = useMutation({
    mutationFn: (values: Values) => unwrap(api.POST("/auth/password-reset/request", { body: values })),
    onError: (err) => setServerErrors(err, form.setError, ["email"]),
  });

  if (request.isSuccess) {
    // The API answers the same way whether or not the account exists, and so do we.
    return (
      <AuthLayout title="Check your email">
        <div className="grid gap-4 text-sm">
          <p>
            If there's an account for <strong>{request.variables.email}</strong>, we've sent it a link to
            reset the password. The link works once and expires in 60 minutes.
          </p>
          <Link to="/login" className="underline underline-offset-4">
            Back to sign in
          </Link>
        </div>
      </AuthLayout>
    );
  }

  return (
    <AuthLayout title="Reset your password" description="We'll email you a link to choose a new password.">
      <form className="grid gap-4" noValidate onSubmit={form.handleSubmit((v) => request.mutate(v))}>
        {errors.root?.server && (
          <Alert variant="destructive">
            <AlertDescription>{errors.root.server.message}</AlertDescription>
          </Alert>
        )}
        <FormField
          label="Email"
          type="email"
          autoComplete="email"
          autoFocus
          error={errors.email?.message}
          {...form.register("email")}
        />
        <Button type="submit" disabled={request.isPending}>
          {request.isPending ? "Sending…" : "Send reset link"}
        </Button>
        <Link to="/login" className="text-center text-sm text-muted-foreground underline-offset-4 hover:underline">
          Back to sign in
        </Link>
      </form>
    </AuthLayout>
  );
}
