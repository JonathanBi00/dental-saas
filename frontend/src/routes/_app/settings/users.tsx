import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { UserPlus } from "lucide-react";
import { useState, type FormEvent } from "react";

import { api, isApiError, unwrap, type Schemas } from "@/api/client";
import { AccessFields } from "@/components/members/access-fields";
import { FormField } from "@/components/form-field";
import { RequirePermission } from "@/components/shell/require-permission";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { emailSchema } from "@/lib/forms";
import { locationsQuery, type Location } from "@/lib/locations";
import { ROLE_LABELS, canBeLocationRestricted, type Role } from "@/lib/roles";
import { sessionQuery, useSession } from "@/lib/session";

export const Route = createFileRoute("/_app/settings/users")({
  component: () => (
    <RequirePermission permission="members:manage">
      <UsersPage />
    </RequirePermission>
  ),
});

type Member = Schemas["Member"];
type Invitation = Schemas["Invitation"];

// MVP: one page of up to 200 members/invitations; paging is a follow-up.
const membersQuery = {
  queryKey: ["members"],
  queryFn: () => unwrap(api.GET("/members", { params: { query: { limit: 200 } } })),
};
const invitationsQuery = {
  queryKey: ["invitations"],
  queryFn: () => unwrap(api.GET("/invitations", { params: { query: { limit: 200 } } })),
};

function UsersPage() {
  const session = useSession();
  const queryClient = useQueryClient();
  const members = useQuery(membersQuery);
  const invitations = useQuery(invitationsQuery);
  const locations = useQuery(locationsQuery(true));
  const [inviting, setInviting] = useState(false);
  const [editing, setEditing] = useState<Member | null>(null);

  const refresh = () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: ["members"] }),
      queryClient.invalidateQueries({ queryKey: ["invitations"] }),
      // Changing your own role or access changes your session.
      queryClient.invalidateQueries({ queryKey: sessionQuery.queryKey }),
    ]);

  const setStatus = useMutation({
    mutationFn: ({ member, action }: { member: Member; action: "deactivate" | "reactivate" }) => {
      const params = { path: { memberId: member.id } };
      return unwrap(
        action === "deactivate"
          ? api.POST("/members/{memberId}/deactivate", { params })
          : api.POST("/members/{memberId}/reactivate", { params }),
      );
    },
    onSuccess: refresh,
  });
  const revoke = useMutation({
    mutationFn: (inv: Invitation) =>
      unwrap(api.POST("/invitations/{invitationId}/revoke", { params: { path: { invitationId: inv.id } } })),
    onSuccess: refresh,
  });

  const allLocations = locations.data ?? [];
  const locationNames = (ids: string[]) =>
    ids.length === 0 ? "All locations" : ids.map((id) => allLocations.find((l) => l.id === id)?.name ?? "Unknown").join(", ");
  const canGrantOwner = session.role === "owner";
  const actionError = setStatus.error ?? revoke.error;

  return (
    <div className="grid max-w-5xl gap-6">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h1 className="text-2xl font-semibold tracking-tight">Users</h1>
        <Button onClick={() => setInviting(true)}>
          <UserPlus aria-hidden />
          Invite someone
        </Button>
      </div>

      {actionError && (
        <Alert variant="destructive">
          <AlertDescription>{actionError.message}</AlertDescription>
        </Alert>
      )}

      <section className="grid gap-2">
        <h2 className="text-lg font-medium">Members</h2>
        {members.isPending ? (
          <p className="text-muted-foreground">Loading…</p>
        ) : members.isError ? (
          <Alert variant="destructive">
            <AlertDescription>Couldn't load members: {members.error.message}</AlertDescription>
          </Alert>
        ) : (
          <div className="overflow-x-auto rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Role</TableHead>
                  <TableHead>Locations</TableHead>
                  <TableHead className="w-0">
                    <span className="sr-only">Actions</span>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {members.data.items.map((m) => {
                  const active = m.status === "active";
                  return (
                    <TableRow key={m.id}>
                      <TableCell>
                        <div className="font-medium">
                          {m.name} {m.user_id === session.user.id && <span className="text-muted-foreground">(you)</span>}{" "}
                          {!active && <Badge variant="secondary">Deactivated</Badge>}
                        </div>
                        <div className="text-muted-foreground">{m.email}</div>
                      </TableCell>
                      <TableCell>{ROLE_LABELS[m.role]}</TableCell>
                      <TableCell className="text-muted-foreground">{locationNames(m.location_ids)}</TableCell>
                      <TableCell>
                        <div className="flex justify-end gap-1">
                          <Button variant="ghost" size="sm" onClick={() => setEditing(m)}>
                            Edit access
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            disabled={setStatus.isPending}
                            onClick={() => {
                              if (!active) return setStatus.mutate({ member: m, action: "reactivate" });
                              if (window.confirm(`Deactivate ${m.name}? They lose access immediately.`))
                                setStatus.mutate({ member: m, action: "deactivate" });
                            }}
                          >
                            {active ? "Deactivate" : "Reactivate"}
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </div>
        )}
      </section>

      <section className="grid gap-2">
        <h2 className="text-lg font-medium">Pending invitations</h2>
        {invitations.isPending ? (
          <p className="text-muted-foreground">Loading…</p>
        ) : invitations.isError ? (
          <Alert variant="destructive">
            <AlertDescription>Couldn't load invitations: {invitations.error.message}</AlertDescription>
          </Alert>
        ) : invitations.data.items.length === 0 ? (
          <p className="text-muted-foreground">No pending invitations.</p>
        ) : (
          <div className="overflow-x-auto rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Email</TableHead>
                  <TableHead>Role</TableHead>
                  <TableHead>Locations</TableHead>
                  <TableHead>Expires</TableHead>
                  <TableHead className="w-0">
                    <span className="sr-only">Actions</span>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {invitations.data.items.map((inv) => (
                  <TableRow key={inv.id}>
                    <TableCell className="font-medium">{inv.email}</TableCell>
                    <TableCell>{ROLE_LABELS[inv.role]}</TableCell>
                    <TableCell className="text-muted-foreground">{locationNames(inv.location_ids)}</TableCell>
                    <TableCell className="text-muted-foreground">{new Date(inv.expires_at).toLocaleString()}</TableCell>
                    <TableCell>
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={revoke.isPending}
                        onClick={() => window.confirm(`Revoke the invitation for ${inv.email}?`) && revoke.mutate(inv)}
                      >
                        Revoke
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </section>

      <Dialog open={inviting} onOpenChange={setInviting}>
        <DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Invite someone</DialogTitle>
            <DialogDescription>They'll get an email with a link to join. The link expires in 7 days.</DialogDescription>
          </DialogHeader>
          {inviting && (
            <InviteForm
              locations={allLocations.filter((l) => !l.archived_at)}
              canGrantOwner={canGrantOwner}
              onDone={async () => {
                await refresh();
                setInviting(false);
              }}
              onCancel={() => setInviting(false)}
            />
          )}
        </DialogContent>
      </Dialog>

      <Dialog open={editing !== null} onOpenChange={(open) => !open && setEditing(null)}>
        <DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Edit access{editing && ` for ${editing.name}`}</DialogTitle>
            <DialogDescription>Changes apply on their next request.</DialogDescription>
          </DialogHeader>
          {editing && (
            <EditAccessForm
              key={editing.id}
              member={editing}
              locations={allLocations.filter((l) => !l.archived_at || editing.location_ids.includes(l.id))}
              canGrantOwner={canGrantOwner}
              onDone={async () => {
                await refresh();
                setEditing(null);
              }}
              onCancel={() => setEditing(null)}
            />
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}

type FormProps = {
  locations: Location[];
  canGrantOwner: boolean;
  onDone: () => Promise<void>;
  onCancel: () => void;
};

function InviteForm({ locations, canGrantOwner, onDone, onCancel }: FormProps) {
  const [email, setEmail] = useState("");
  const [emailError, setEmailError] = useState<string>();
  const [role, setRole] = useState<Role>("front_desk");
  const [locationIds, setLocationIds] = useState<string[]>([]);
  const invite = useMutation({
    mutationFn: (body: Schemas["InvitationCreate"]) => unwrap(api.POST("/invitations", { body })),
    onSuccess: onDone,
    onError: (err) => isApiError(err, 422) && err.fieldErrors.email && setEmailError(`Email ${err.fieldErrors.email}`),
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const parsed = emailSchema.safeParse(email);
    if (!parsed.success) return setEmailError(parsed.error.issues[0].message);
    setEmailError(undefined);
    invite.mutate({ email: parsed.data, role, location_ids: canBeLocationRestricted(role) ? locationIds : [] });
  };

  return (
    <form className="grid gap-4" noValidate onSubmit={submit}>
      {invite.isError && !emailError && (
        <Alert variant="destructive">
          <AlertDescription>{invite.error.message}</AlertDescription>
        </Alert>
      )}
      <FormField
        label="Email"
        type="email"
        autoFocus
        value={email}
        onChange={(e) => setEmail(e.target.value)}
        error={emailError}
      />
      <AccessFields
        idPrefix="invite"
        role={role}
        locationIds={locationIds}
        onRoleChange={setRole}
        onLocationIdsChange={setLocationIds}
        locations={locations}
        canGrantOwner={canGrantOwner}
      />
      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={invite.isPending}>
          {invite.isPending ? "Sending…" : "Send invitation"}
        </Button>
      </div>
    </form>
  );
}

function EditAccessForm({ member, locations, canGrantOwner, onDone, onCancel }: FormProps & { member: Member }) {
  const [role, setRole] = useState<Role>(member.role);
  const [locationIds, setLocationIds] = useState<string[]>(member.location_ids);
  const update = useMutation({
    mutationFn: (body: Schemas["MemberUpdate"]) =>
      unwrap(api.PATCH("/members/{memberId}", { params: { path: { memberId: member.id } }, body })),
    onSuccess: onDone,
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    update.mutate({ role, location_ids: canBeLocationRestricted(role) ? locationIds : [] });
  };

  return (
    <form className="grid gap-4" onSubmit={submit}>
      {update.isError && (
        <Alert variant="destructive">
          <AlertDescription>{update.error.message}</AlertDescription>
        </Alert>
      )}
      <AccessFields
        idPrefix="member"
        role={role}
        locationIds={locationIds}
        onRoleChange={setRole}
        onLocationIdsChange={setLocationIds}
        locations={locations}
        canGrantOwner={canGrantOwner}
      />
      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={update.isPending}>
          {update.isPending ? "Saving…" : "Save"}
        </Button>
      </div>
    </form>
  );
}
