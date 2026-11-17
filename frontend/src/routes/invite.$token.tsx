import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, createFileRoute, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { api, isApiError, unwrap, type Schemas } from "@/api/client";
import { AuthLayout } from "@/components/auth-layout";
import { FormField } from "@/components/form-field";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { MIN_PASSWORD_LENGTH, newPasswordForm, setServerErrors } from "@/lib/forms";
import { ROLE_LABELS } from "@/lib/roles";
import { sessionQuery, type Session } from "@/lib/session";

export const Route = createFileRoute("/invite/$token")({
  component: AcceptInvitePage,
});

type Preview = Schemas["InvitationPreview"];

function AcceptInvitePage() {
  const { token } = Route.useParams();
  const preview = useQuery({
    queryKey: ["invitation-preview", token],
    queryFn: () => unwrap(api.GET("/auth/invitations/{token}", { params: { path: { token } } })),
  });

  if (preview.isPending) {
    return <AuthLayout title="Invitation">Checking your invitation…</AuthLayout>;
  }
  if (preview.isError) {
    const gone = isApiError(preview.error, 404) || isApiError(preview.error, 410);
    return (
      <AuthLayout title={gone ? "Invitation no longer valid" : "Something went wrong"}>
        <div className="grid gap-4 text-sm">
          <p>
            {gone
              ? "This invitation has expired, been revoked or already been used. Ask your practice to send a new one."
              : "We couldn't load this invitation. Try again in a moment."}
          </p>
          <Link to="/login" className="underline underline-offset-4">
            Go to sign in
          </Link>
        </div>
      </AuthLayout>
    );
  }

  const invite = preview.data;
  return (
    <AuthLayout
      title={`Join ${invite.organisation_name}`}
      description={
        <>
          You've been invited as <strong>{ROLE_LABELS[invite.role]}</strong> using {invite.email}.
        </>
      }
    >
      {invite.existing_account ? (
        <ExistingAccountForm token={token} invite={invite} />
      ) : (
        <NewAccountForm token={token} invite={invite} />
      )}
    </AuthLayout>
  );
}

/** Signs in with the new session and opens the app. */
function useAccept(token: string) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  return {
    mutationFn: (body: Schemas["AcceptInvitationRequest"]) =>
      unwrap(api.POST("/auth/invitations/{token}/accept", { params: { path: { token } }, body })),
    onSuccess: async (session: Session) => {
      queryClient.clear();
      queryClient.setQueryData(sessionQuery.queryKey, session);
      await navigate({ to: "/", replace: true });
    },
  };
}

const newAccountSchema = (email: string) =>
  newPasswordForm({ name: z.string().trim().min(1, "Enter your name") }, email);
type NewAccountValues = z.infer<ReturnType<typeof newAccountSchema>>;

function NewAccountForm({ token, invite }: { token: string; invite: Preview }) {
  const [schema] = useState(() => newAccountSchema(invite.email));
  const form = useForm<NewAccountValues>({
    resolver: zodResolver(schema),
    defaultValues: { name: "", password: "", confirm: "" },
  });
  const { errors } = form.formState;
  const accept = useMutation({
    ...useAccept(token),
    onError: (err) => setServerErrors(err, form.setError, ["name", "password"]),
  });

  return (
    <form
      className="grid gap-4"
      noValidate
      onSubmit={form.handleSubmit(({ name, password }) => accept.mutate({ name, password }))}
    >
      {errors.root?.server && (
        <Alert variant="destructive">
          <AlertDescription>{errors.root.server.message}</AlertDescription>
        </Alert>
      )}
      <FormField label="Your name" autoComplete="name" autoFocus error={errors.name?.message} {...form.register("name")} />
      <FormField
        label="Password"
        type="password"
        autoComplete="new-password"
        hint={`At least ${MIN_PASSWORD_LENGTH} characters.`}
        error={errors.password?.message}
        {...form.register("password")}
      />
      <FormField
        label="Confirm password"
        type="password"
        autoComplete="new-password"
        error={errors.confirm?.message}
        {...form.register("confirm")}
      />
      <Button type="submit" disabled={accept.isPending}>
        {accept.isPending ? "Creating your account…" : "Create account and join"}
      </Button>
    </form>
  );
}

const existingAccountSchema = z.object({ password: z.string().min(1, "Enter your password") });

function ExistingAccountForm({ token, invite }: { token: string; invite: Preview }) {
  const form = useForm<z.infer<typeof existingAccountSchema>>({
    resolver: zodResolver(existingAccountSchema),
    defaultValues: { password: "" },
  });
  const { errors } = form.formState;
  const accept = useMutation({
    ...useAccept(token),
    onError: (err) => setServerErrors(err, form.setError, ["password"]),
  });

  return (
    <form className="grid gap-4" noValidate onSubmit={form.handleSubmit((v) => accept.mutate(v))}>
      <p className="text-sm text-muted-foreground">
        You already have an account. Enter its password to add {invite.organisation_name} to it.
      </p>
      {errors.root?.server && (
        <Alert variant="destructive">
          <AlertDescription>{errors.root.server.message}</AlertDescription>
        </Alert>
      )}
      <FormField
        label="Password"
        type="password"
        autoComplete="current-password"
        autoFocus
        error={errors.password?.message}
        {...form.register("password")}
      />
      <Button type="submit" disabled={accept.isPending}>
        {accept.isPending ? "Joining…" : "Join"}
      </Button>
      <Link to="/forgot-password" className="text-center text-sm text-muted-foreground underline-offset-4 hover:underline">
        Forgot your password?
      </Link>
    </form>
  );
}
