import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { Plus } from "lucide-react";
import { useState } from "react";

import { api, unwrap } from "@/api/client";
import { LocationForm, type LocationValues } from "@/components/locations/location-form";
import { RequirePermission } from "@/components/shell/require-permission";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { locationsQuery, type Location } from "@/lib/locations";
import { sessionQuery } from "@/lib/session";

export const Route = createFileRoute("/_app/settings/locations")({
  component: () => (
    <RequirePermission permission="locations:manage">
      <LocationsPage />
    </RequirePermission>
  ),
});

type Editing = { mode: "create" } | { mode: "edit"; location: Location } | null;

function LocationsPage() {
  const queryClient = useQueryClient();
  const [showArchived, setShowArchived] = useState(false);
  const [editing, setEditing] = useState<Editing>(null);
  const locations = useQuery(locationsQuery(showArchived));

  // Locations feed the header switcher and the session's accessible locations too.
  const refresh = () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: ["locations"] }),
      queryClient.invalidateQueries({ queryKey: sessionQuery.queryKey }),
    ]);

  const save = useMutation({
    mutationFn: (values: LocationValues) =>
      editing?.mode === "edit"
        ? unwrap(api.PATCH("/locations/{locationId}", { params: { path: { locationId: editing.location.id } }, body: values }))
        : unwrap(api.POST("/locations", { body: values })),
    onSuccess: async () => {
      await refresh();
      setEditing(null);
    },
  });

  const archive = useMutation({
    mutationFn: (id: string) => unwrap(api.POST("/locations/{locationId}/archive", { params: { path: { locationId: id } } })),
    onSuccess: refresh,
  });

  return (
    <div className="grid max-w-5xl gap-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h1 className="text-2xl font-semibold tracking-tight">Locations</h1>
        <Button onClick={() => setEditing({ mode: "create" })}>
          <Plus aria-hidden />
          Add location
        </Button>
      </div>

      <div className="flex items-center gap-2">
        <Checkbox id="show-archived" checked={showArchived} onCheckedChange={(v) => setShowArchived(v === true)} />
        <Label htmlFor="show-archived">Show archived</Label>
      </div>

      {archive.isError && (
        <Alert variant="destructive">
          <AlertDescription>{archive.error.message}</AlertDescription>
        </Alert>
      )}

      {locations.isPending ? (
        <p className="text-muted-foreground">Loading…</p>
      ) : locations.isError ? (
        <Alert variant="destructive">
          <AlertDescription>Couldn't load locations: {locations.error.message}</AlertDescription>
        </Alert>
      ) : locations.data.length === 0 ? (
        <p className="text-muted-foreground">No locations yet.</p>
      ) : (
        <div className="overflow-x-auto rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Address</TableHead>
                <TableHead>Contact</TableHead>
                <TableHead>Timezone</TableHead>
                <TableHead className="w-0">
                  <span className="sr-only">Actions</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {locations.data.map((l) => (
                <TableRow key={l.id}>
                  <TableCell className="font-medium">
                    {l.name} {l.archived_at && <Badge variant="secondary">Archived</Badge>}
                  </TableCell>
                  <TableCell className="text-muted-foreground">{formatAddress(l)}</TableCell>
                  <TableCell className="text-muted-foreground">{[l.phone, l.email].filter(Boolean).join(" · ")}</TableCell>
                  <TableCell className="text-muted-foreground">{l.timezone}</TableCell>
                  <TableCell>
                    {!l.archived_at && (
                      <div className="flex justify-end gap-1">
                        <Button variant="ghost" size="sm" onClick={() => setEditing({ mode: "edit", location: l })}>
                          Edit
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={archive.isPending}
                          onClick={() => {
                            if (window.confirm(`Archive ${l.name}? It will be hidden from everyday use.`)) archive.mutate(l.id);
                          }}
                        >
                          Archive
                        </Button>
                      </div>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <Dialog open={editing !== null} onOpenChange={(open) => !open && setEditing(null)}>
        <DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{editing?.mode === "edit" ? `Edit ${editing.location.name}` : "Add location"}</DialogTitle>
            <DialogDescription>Appointment times at this location use its timezone.</DialogDescription>
          </DialogHeader>
          {editing && (
            <LocationForm
              key={editing.mode === "edit" ? editing.location.id : "new"}
              location={editing.mode === "edit" ? editing.location : undefined}
              submitLabel={editing.mode === "edit" ? "Save changes" : "Add location"}
              onSubmit={save.mutateAsync}
              onCancel={() => setEditing(null)}
            />
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}

function formatAddress(l: Location) {
  return [l.address_line1, l.address_line2, [l.suburb, l.state, l.postcode].filter(Boolean).join(" ")]
    .filter(Boolean)
    .join(", ");
}
