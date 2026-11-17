import { Link } from "@tanstack/react-router";
import { History, Home, MapPin, Users, type LucideIcon } from "lucide-react";

import { hasPermission, type Session } from "@/lib/session";

type NavItem = { to: "/" | "/settings/locations" | "/settings/users" | "/settings/audit"; label: string; icon: LucideIcon; permission?: string };

const MAIN: NavItem[] = [{ to: "/", label: "Home", icon: Home }];

// Each settings screen is shown only to roles whose permissions allow it (owner and admin today).
const SETTINGS: NavItem[] = [
  { to: "/settings/locations", label: "Locations", icon: MapPin, permission: "locations:manage" },
  { to: "/settings/users", label: "Users", icon: Users, permission: "members:manage" },
  { to: "/settings/audit", label: "Audit log", icon: History, permission: "audit:read" },
];

export function AppNav({ session }: { session: Session }) {
  const settings = SETTINGS.filter((item) => !item.permission || hasPermission(session, item.permission));

  return (
    <nav aria-label="Main" className="grid gap-4 text-sm">
      <NavList items={MAIN} />
      {settings.length > 0 && (
        <div className="grid gap-1">
          <p className="px-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">Settings</p>
          <NavList items={settings} />
        </div>
      )}
    </nav>
  );
}

function NavList({ items }: { items: NavItem[] }) {
  return (
    <ul className="grid gap-0.5">
      {items.map(({ to, label, icon: Icon }) => (
        <li key={to}>
          <Link
            to={to}
            activeOptions={{ exact: to === "/" }}
            className="flex items-center gap-2 rounded-md px-2 py-1.5 text-muted-foreground hover:bg-accent hover:text-accent-foreground data-[status=active]:bg-accent data-[status=active]:font-medium data-[status=active]:text-foreground"
          >
            <Icon className="size-4" aria-hidden />
            {label}
          </Link>
        </li>
      ))}
    </ul>
  );
}
