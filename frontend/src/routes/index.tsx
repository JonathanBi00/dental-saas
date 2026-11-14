import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";

import { Button } from "@/components/ui/button";

export const Route = createFileRoute("/")({
  component: Placeholder,
});

// Confirms the Vite → API proxy works by fetching the spec the API serves.
async function fetchApiStatus() {
  const res = await fetch("/api/v1/openapi.yaml");
  if (!res.ok) throw new Error(`API responded ${res.status}`);
  return "reachable";
}

function Placeholder() {
  const api = useQuery({ queryKey: ["api-status"], queryFn: fetchApiStatus });

  return (
    <main className="min-h-svh grid place-items-center bg-background p-6 text-foreground">
      <div className="flex max-w-md flex-col gap-4 text-center">
        <h1 className="text-3xl font-semibold tracking-tight">Dental SaaS</h1>
        <p className="text-muted-foreground">
          Frontend scaffold. Sign-in and the app shell arrive in task group 7.
        </p>
        <p className="text-sm">
          API:{" "}
          {api.isPending ? "checking…" : api.isSuccess ? (
            <span className="text-emerald-600">{api.data}</span>
          ) : (
            <span className="text-destructive">unreachable (is `task dev` running the API?)</span>
          )}
        </p>
        <div>
          <Button onClick={() => api.refetch()}>Check again</Button>
        </div>
      </div>
    </main>
  );
}
