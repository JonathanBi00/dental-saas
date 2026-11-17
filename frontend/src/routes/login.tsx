import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, createFileRoute, useNavigate } from "@tanstack/react-router";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { api, unwrap } from "@/api/client";
import { AuthLayout } from "@/components/auth-layout";
import { FormField } from "@/components/form-field";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { safeRedirect } from "@/lib/auth";
import { emailSchema, setServerErrors } from "@/lib/forms";
import { sessionQuery } from "@/lib/session";

export const Route = createFileRoute("/login")({
  validateSearch: (search: Record<string, unknown>): { redirect?: string } => ({
    redirect: typeof search.redirect === "string" ? search.redirect : undefined,
  }),
  component: LoginPage,
});

const schema = z.object({
  email: emailSchema,
  password: z.string().min(1, "Enter your password"),
});
type Values = z.infer<typeof schema>;

function LoginPage() {
  const { redirect } = Route.useSearch();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: { email: "", password: "" } });
  const { errors } = form.formState;

  const signIn = useMutation({
    mutationFn: (values: Values) => unwrap(api.POST("/auth/login", { body: values })),
    onSuccess: async (session) => {
      // Drop anything cached under a previous session before showing the app.
      queryClient.clear();
      queryClient.setQueryData(sessionQuery.queryKey, session);
      await navigate({ to: safeRedirect(redirect), replace: true });
    },
    onError: (err) => {
      form.resetField("password");
      setServerErrors(err, form.setError, ["email", "password"]);
    },
  });

  return (
    <AuthLayout title="Sign in" description="Use the email address your practice invited.">
      <form className="grid gap-4" noValidate onSubmit={form.handleSubmit((v) => signIn.mutate(v))}>
        {errors.root?.server && (
          <Alert variant="destructive">
            <AlertDescription>{errors.root.server.message}</AlertDescription>
          </Alert>
        )}
        <FormField
          label="Email"
          type="email"
          autoComplete="username"
          autoFocus
          error={errors.email?.message}
          {...form.register("email")}
        />
        <FormField
          label="Password"
          type="password"
          autoComplete="current-password"
          error={errors.password?.message}
          {...form.register("password")}
        />
        <Button type="submit" disabled={signIn.isPending}>
          {signIn.isPending ? "Signing in…" : "Sign in"}
        </Button>
        <Link to="/forgot-password" className="text-center text-sm text-muted-foreground underline-offset-4 hover:underline">
          Forgot your password?
        </Link>
      </form>
    </AuthLayout>
  );
}
