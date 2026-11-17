import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useState, type FormEvent, type ReactNode } from "react";

import { api, unwrap, type Schemas } from "@/api/client";
import { RequirePermission } from "@/components/shell/require-permission";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { NativeSelect } from "@/components/ui/native-select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { hasPermission, useSession } from "@/lib/session";

export const Route = createFileRoute("/_app/settings/audit")({
  component: () => (
    <RequirePermission permission="audit:read">
      <AuditPage />
    </RequirePermission>
  ),
});

type AuditEvent = Schemas["AuditEvent"];

type Filters = { from: string; to: string; actor: string; action: string; targetType: string };
const noFilters: Filters = { from: "", to: "", actor: "", action: "", targetType: "" };

const PAGE_SIZE = 50;

function AuditPage() {
  const session = useSession();
  const [draft, setDraft] = useState<Filters>(noFilters);
  const [filters, setFilters] = useState<Filters>(noFilters);

  // Actor choices come from the member list (owners and admins can read both today).
  const members = useQuery({
    queryKey: ["members"],
    queryFn: () => unwrap(api.GET("/members", { params: { query: { limit: 200 } } })),
    enabled: hasPermission(session, "members:manage"),
  });

  const events = useInfiniteQuery({
    queryKey: ["audit-events", filters],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET("/audit-events", {
          params: {
            query: {
              limit: PAGE_SIZE,
              cursor: pageParam,
              // Dates are picked in the browser's timezone; the API takes UTC instants.
              from: filters.from ? new Date(`${filters.from}T00:00:00`).toISOString() : undefined,
              to: filters.to ? nextDay(filters.to).toISOString() : undefined,
              actor_user_id: filters.actor || undefined,
              action: filters.action.trim() || undefined,
              target_type: filters.targetType.trim() || undefined,
            },
          },
        }),
      ),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });

  const apply = (e: FormEvent) => {
    e.preventDefault();
    setFilters(draft);
  };
  const set = (key: keyof Filters) => (value: string) => setDraft((d) => ({ ...d, [key]: value }));
  const rows = events.data?.pages.flatMap((p) => p.items) ?? [];

  return (
    <div className="grid max-w-6xl gap-4">
      <h1 className="text-2xl font-semibold tracking-tight">Audit log</h1>

      <form onSubmit={apply} className="grid gap-3 rounded-md border p-3 sm:grid-cols-3 lg:grid-cols-6 lg:items-end">
        <Filter id="audit-from" label="From">
          <Input id="audit-from" type="date" value={draft.from} onChange={(e) => set("from")(e.target.value)} />
        </Filter>
        <Filter id="audit-to" label="To">
          <Input id="audit-to" type="date" value={draft.to} onChange={(e) => set("to")(e.target.value)} />
        </Filter>
        <Filter id="audit-actor" label="Who">
          <NativeSelect id="audit-actor" value={draft.actor} onChange={(e) => set("actor")(e.target.value)}>
            <option value="">Anyone</option>
            {members.data?.items.map((m) => (
              <option key={m.user_id} value={m.user_id}>
                {m.name}
              </option>
            ))}
          </NativeSelect>
        </Filter>
        <Filter id="audit-action" label="Action">
          <Input
            id="audit-action"
            placeholder="e.g. location.updated"
            value={draft.action}
            onChange={(e) => set("action")(e.target.value)}
          />
        </Filter>
        <Filter id="audit-target" label="Record type">
          <Input
            id="audit-target"
            placeholder="e.g. location"
            value={draft.targetType}
            onChange={(e) => set("targetType")(e.target.value)}
          />
        </Filter>
        <div className="flex gap-2">
          <Button type="submit">Apply</Button>
          <Button
            type="button"
            variant="ghost"
            onClick={() => {
              setDraft(noFilters);
              setFilters(noFilters);
            }}
          >
            Clear
          </Button>
        </div>
      </form>

      {events.isPending ? (
        <p className="text-muted-foreground">Loading…</p>
      ) : events.isError ? (
        <Alert variant="destructive">
          <AlertDescription>Couldn't load the audit log: {events.error.message}</AlertDescription>
        </Alert>
      ) : rows.length === 0 ? (
        <p className="text-muted-foreground">No events match these filters.</p>
      ) : (
        <>
          <div className="overflow-x-auto rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>When</TableHead>
                  <TableHead>Who</TableHead>
                  <TableHead>Action</TableHead>
                  <TableHead>Record</TableHead>
                  <TableHead>Details</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((e) => (
                  <EventRow key={e.id} event={e} />
                ))}
              </TableBody>
            </Table>
          </div>
          {events.hasNextPage && (
            <div>
              <Button variant="outline" onClick={() => events.fetchNextPage()} disabled={events.isFetchingNextPage}>
                {events.isFetchingNextPage ? "Loading…" : "Load more"}
              </Button>
            </div>
          )}
        </>
      )}
    </div>
  );
}

function Filter({ id, label, children }: { id: string; label: string; children: ReactNode }) {
  return (
    <div className="grid gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      {children}
    </div>
  );
}

function EventRow({ event: e }: { event: AuditEvent }) {
  const hasDetails = Object.keys(e.details ?? {}).length > 0;
  return (
    <TableRow className="align-top">
      <TableCell className="whitespace-nowrap">{new Date(e.created_at).toLocaleString()}</TableCell>
      <TableCell>
        {e.actor ? (
          <>
            <div>{e.actor.name}</div>
            <div className="text-muted-foreground">{e.actor.email}</div>
          </>
        ) : (
          <span className="text-muted-foreground">System / unknown</span>
        )}
      </TableCell>
      <TableCell className="font-mono text-xs">{e.action}</TableCell>
      <TableCell className="text-muted-foreground">
        {e.target_type ?? "—"}
        {e.target_id && <div className="font-mono text-xs">{e.target_id}</div>}
      </TableCell>
      <TableCell className="max-w-sm">
        {hasDetails ? (
          <details>
            <summary className="cursor-pointer text-muted-foreground">Show</summary>
            <pre className="mt-1 overflow-x-auto text-xs whitespace-pre-wrap">{JSON.stringify(e.details, null, 2)}</pre>
          </details>
        ) : (
          <span className="text-muted-foreground">—</span>
        )}
      </TableCell>
    </TableRow>
  );
}

/** Local midnight at the start of the day after `date` (YYYY-MM-DD), so "To" includes that whole day. */
function nextDay(date: string) {
  const d = new Date(`${date}T00:00:00`);
  d.setDate(d.getDate() + 1);
  return d;
}
