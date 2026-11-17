import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { FormField } from "@/components/form-field";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { NativeSelect } from "@/components/ui/native-select";
import { setServerErrors } from "@/lib/forms";
import type { Location } from "@/lib/locations";

const STATES = ["NSW", "VIC", "QLD", "SA", "WA", "TAS", "NT", "ACT"];

const AU_TIMEZONES = [
  "Australia/Sydney",
  "Australia/Melbourne",
  "Australia/Brisbane",
  "Australia/Adelaide",
  "Australia/Darwin",
  "Australia/Perth",
  "Australia/Hobart",
  "Australia/Lord_Howe",
];

const optionalEmail = z
  .string()
  .trim()
  .refine((v) => v === "" || z.email().safeParse(v).success, "Enter a valid email address");

const schema = z.object({
  name: z.string().trim().min(1, "Enter a name"),
  address_line1: z.string().trim(),
  address_line2: z.string().trim(),
  suburb: z.string().trim(),
  state: z.string(),
  postcode: z
    .string()
    .trim()
    .refine((v) => v === "" || /^\d{4}$/.test(v), "Use a 4-digit postcode"),
  phone: z.string().trim(),
  email: optionalEmail,
  timezone: z.string().min(1, "Choose a timezone"),
});
export type LocationValues = z.infer<typeof schema>;

const FIELDS = Object.keys(schema.shape) as (keyof LocationValues)[];

const blank: LocationValues = {
  name: "",
  address_line1: "",
  address_line2: "",
  suburb: "",
  state: "NSW",
  postcode: "",
  phone: "",
  email: "",
  timezone: "Australia/Sydney",
};

export function LocationForm({
  location,
  submitLabel,
  onSubmit,
  onCancel,
}: {
  location?: Location;
  submitLabel: string;
  onSubmit: (values: LocationValues) => Promise<unknown>;
  onCancel: () => void;
}) {
  const form = useForm<LocationValues>({
    resolver: zodResolver(schema),
    defaultValues: location ? pick(location) : blank,
  });
  const { errors, isSubmitting } = form.formState;
  const timezones = location && !AU_TIMEZONES.includes(location.timezone)
    ? [location.timezone, ...AU_TIMEZONES]
    : AU_TIMEZONES;

  const submit = form.handleSubmit(async (values) => {
    try {
      await onSubmit(values);
    } catch (err) {
      setServerErrors(err, form.setError, FIELDS);
    }
  });

  return (
    <form className="grid gap-4" noValidate onSubmit={submit}>
      {errors.root?.server && (
        <Alert variant="destructive">
          <AlertDescription>{errors.root.server.message}</AlertDescription>
        </Alert>
      )}
      <FormField label="Name" autoFocus error={errors.name?.message} {...form.register("name")} />
      <FormField label="Address" autoComplete="address-line1" error={errors.address_line1?.message} {...form.register("address_line1")} />
      <FormField
        label="Address line 2"
        autoComplete="address-line2"
        error={errors.address_line2?.message}
        {...form.register("address_line2")}
      />
      <div className="grid gap-4 sm:grid-cols-[1fr_6rem_6rem]">
        <FormField label="Suburb" error={errors.suburb?.message} {...form.register("suburb")} />
        <div className="grid gap-1.5">
          <Label htmlFor="location-state">State</Label>
          <NativeSelect id="location-state" {...form.register("state")}>
            <option value="">—</option>
            {STATES.map((s) => (
              <option key={s}>{s}</option>
            ))}
          </NativeSelect>
        </div>
        <FormField label="Postcode" inputMode="numeric" error={errors.postcode?.message} {...form.register("postcode")} />
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        <FormField label="Phone" type="tel" error={errors.phone?.message} {...form.register("phone")} />
        <FormField label="Email" type="email" error={errors.email?.message} {...form.register("email")} />
      </div>
      <div className="grid gap-1.5">
        <Label htmlFor="location-timezone">Timezone</Label>
        <NativeSelect
          id="location-timezone"
          aria-invalid={errors.timezone ? true : undefined}
          {...form.register("timezone")}
        >
          {timezones.map((tz) => (
            <option key={tz} value={tz}>
              {tz.replace("Australia/", "").replace("_", " ")} ({tz})
            </option>
          ))}
        </NativeSelect>
        {errors.timezone && <p className="text-sm text-destructive">{errors.timezone.message}</p>}
      </div>
      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={isSubmitting}>
          {isSubmitting ? "Saving…" : submitLabel}
        </Button>
      </div>
    </form>
  );
}

function pick(l: Location): LocationValues {
  return {
    name: l.name,
    address_line1: l.address_line1,
    address_line2: l.address_line2,
    suburb: l.suburb,
    state: l.state,
    postcode: l.postcode,
    phone: l.phone,
    email: l.email,
    timezone: l.timezone,
  };
}
