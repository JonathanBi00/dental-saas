// Package tenancy manages organisations (tenants) and their locations.
package tenancy

import (
	"context"
	"net/mail"
	"strings"
	"time"
	_ "time/tzdata" // IANA zone database embedded so validation works on every OS

	"github.com/google/uuid"

	"dentalsaas/internal/access"
	"dentalsaas/internal/platform/apperr"
	"dentalsaas/internal/platform/audit"
	"dentalsaas/internal/platform/db"
	"dentalsaas/internal/platform/httpx"
	"dentalsaas/internal/platform/ids"
	"dentalsaas/internal/store"
)

type Service struct {
	DB  *db.Pool
	Now func() time.Time
}

func New(pool *db.Pool) *Service { return &Service{DB: pool, Now: time.Now} }

// ---------------------------------------------------------------- organisations

// CreateOrganisation is an operator action (CLI), not exposed over the API.
func (s *Service) CreateOrganisation(ctx context.Context, name, abn string) (store.Organisation, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return store.Organisation{}, apperr.Validation(apperr.FieldError{Field: "name", Message: "is required"})
	}
	id := ids.New()
	var org store.Organisation
	err := s.DB.InTenantTx(ctx, db.Tenant{OrgID: id}, func(tx *db.Tx) error {
		var abnPtr *string
		if abn = strings.TrimSpace(abn); abn != "" {
			abnPtr = &abn
		}
		var err error
		org, err = tx.CreateOrganisation(ctx, store.CreateOrganisationParams{ID: id, Name: name, Abn: abnPtr})
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			OrgID: id, Action: "organisation.created", TargetType: "organisation", TargetID: id,
			Details: map[string]any{"name": name},
		})
	})
	return org, err
}

// -------------------------------------------------------------------- locations

// LocationInput carries create/update fields. Nil means "not provided".
type LocationInput struct {
	Name, AddressLine1, AddressLine2, Suburb, State, Postcode, Phone, Email, Timezone *string
}

type fields struct {
	Name, AddressLine1, AddressLine2, Suburb, State, Postcode, Phone, Email, Timezone string
}

func fieldsOf(l store.Location) fields {
	return fields{l.Name, l.AddressLine1, l.AddressLine2, l.Suburb, l.State, l.Postcode, l.Phone, l.Email, l.Timezone}
}

// apply overlays provided input values (trimmed) onto f.
func (f fields) apply(in LocationInput) fields {
	set := func(dst *string, src *string) {
		if src != nil {
			*dst = strings.TrimSpace(*src)
		}
	}
	set(&f.Name, in.Name)
	set(&f.AddressLine1, in.AddressLine1)
	set(&f.AddressLine2, in.AddressLine2)
	set(&f.Suburb, in.Suburb)
	set(&f.State, in.State)
	set(&f.Postcode, in.Postcode)
	set(&f.Phone, in.Phone)
	set(&f.Email, in.Email)
	set(&f.Timezone, in.Timezone)
	return f
}

const maxFieldLen = 200

func (f fields) validate() error {
	var errs apperr.Fields
	if f.Name == "" {
		errs.Add("name", "is required")
	}
	for name, v := range map[string]string{
		"name": f.Name, "address_line1": f.AddressLine1, "address_line2": f.AddressLine2,
		"suburb": f.Suburb, "state": f.State, "postcode": f.Postcode, "phone": f.Phone, "email": f.Email,
	} {
		if len(v) > maxFieldLen {
			errs.Add(name, "must be at most 200 characters")
		}
	}
	if f.Email != "" {
		if a, err := mail.ParseAddress(f.Email); err != nil || a.Address != f.Email {
			errs.Add("email", "is not a valid email address")
		}
	}
	if !ValidTimezone(f.Timezone) {
		errs.Add("timezone", "must be an IANA timezone name such as Australia/Sydney")
	}
	return errs.Err()
}

// ValidTimezone reports whether tz is a loadable IANA zone name.
func ValidTimezone(tz string) bool {
	if tz == "" || tz == "Local" || !strings.Contains(tz, "/") && tz != "UTC" {
		return false
	}
	_, err := time.LoadLocation(tz)
	return err == nil
}

func (s *Service) CreateLocation(ctx context.Context, in LocationInput) (store.Location, error) {
	p, err := access.Require(ctx, access.PermLocationsManage)
	if err != nil {
		return store.Location{}, err
	}
	f := fields{}.apply(in)
	if err := f.validate(); err != nil {
		return store.Location{}, err
	}

	var loc store.Location
	err = s.DB.InTenantTx(ctx, p.Tenant(), func(tx *db.Tx) error {
		var err error
		loc, err = tx.CreateLocation(ctx, store.CreateLocationParams{
			ID: ids.New(), OrgID: p.OrgID, Name: f.Name,
			AddressLine1: f.AddressLine1, AddressLine2: f.AddressLine2, Suburb: f.Suburb,
			State: f.State, Postcode: f.Postcode, Phone: f.Phone, Email: f.Email, Timezone: f.Timezone,
		})
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			OrgID: p.OrgID, ActorID: p.UserID, Action: "location.created",
			TargetType: "location", TargetID: loc.ID,
			Details: map[string]any{"name": loc.Name, "timezone": loc.Timezone},
		})
	})
	return loc, err
}

// getAccessible loads a location, treating anything outside the caller's scope as not found.
func getAccessible(ctx context.Context, tx *db.Tx, p access.Principal, id uuid.UUID) (store.Location, error) {
	loc, err := tx.GetLocation(ctx, id)
	if db.IsNoRows(err) || (err == nil && !p.CanAccessLocation(id)) {
		return store.Location{}, apperr.NotFound("Location")
	}
	return loc, err
}

func (s *Service) GetLocation(ctx context.Context, id uuid.UUID) (store.Location, error) {
	p, err := access.Require(ctx, access.PermLocationsRead)
	if err != nil {
		return store.Location{}, err
	}
	var loc store.Location
	err = s.DB.InTenantTx(ctx, p.Tenant(), func(tx *db.Tx) error {
		loc, err = getAccessible(ctx, tx, p, id)
		return err
	})
	return loc, err
}

func (s *Service) UpdateLocation(ctx context.Context, id uuid.UUID, in LocationInput) (store.Location, error) {
	p, err := access.Require(ctx, access.PermLocationsManage)
	if err != nil {
		return store.Location{}, err
	}
	var loc store.Location
	err = s.DB.InTenantTx(ctx, p.Tenant(), func(tx *db.Tx) error {
		cur, err := getAccessible(ctx, tx, p, id)
		if err != nil {
			return err
		}
		old := fieldsOf(cur)
		f := old.apply(in)
		if err := f.validate(); err != nil {
			return err
		}
		if f == old {
			loc = cur
			return nil
		}
		loc, err = tx.UpdateLocation(ctx, store.UpdateLocationParams{
			ID: id, Name: f.Name, AddressLine1: f.AddressLine1, AddressLine2: f.AddressLine2,
			Suburb: f.Suburb, State: f.State, Postcode: f.Postcode, Phone: f.Phone, Email: f.Email,
			Timezone: f.Timezone,
		})
		if err != nil {
			return err
		}
		ch := audit.Changes{}
		ch.Diff("name", old.Name, f.Name)
		ch.Diff("address_line1", old.AddressLine1, f.AddressLine1)
		ch.Diff("address_line2", old.AddressLine2, f.AddressLine2)
		ch.Diff("suburb", old.Suburb, f.Suburb)
		ch.Diff("state", old.State, f.State)
		ch.Diff("postcode", old.Postcode, f.Postcode)
		ch.Diff("phone", old.Phone, f.Phone)
		ch.Diff("email", old.Email, f.Email)
		ch.Diff("timezone", old.Timezone, f.Timezone)
		return audit.Record(ctx, tx, audit.Event{
			OrgID: p.OrgID, ActorID: p.UserID, Action: "location.updated",
			TargetType: "location", TargetID: id, Details: map[string]any{"changes": ch},
		})
	})
	return loc, err
}

func (s *Service) ArchiveLocation(ctx context.Context, id uuid.UUID) (store.Location, error) {
	p, err := access.Require(ctx, access.PermLocationsManage)
	if err != nil {
		return store.Location{}, err
	}
	var loc store.Location
	err = s.DB.InTenantTx(ctx, p.Tenant(), func(tx *db.Tx) error {
		cur, err := getAccessible(ctx, tx, p, id)
		if err != nil {
			return err
		}
		if cur.ArchivedAt != nil {
			return apperr.Conflict("already_archived", "Location is already archived.")
		}
		if loc, err = tx.ArchiveLocation(ctx, id); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			OrgID: p.OrgID, ActorID: p.UserID, Action: "location.archived",
			TargetType: "location", TargetID: id, Details: map[string]any{"name": cur.Name},
		})
	})
	return loc, err
}

type ListLocationsParams struct {
	Limit           *int
	Cursor          *string
	UpdatedSince    *time.Time
	IncludeArchived bool
}

type ListLocationsResult struct {
	Items      []store.Location
	NextCursor *string
	SyncUntil  *time.Time // set in delta-sync mode
}

func (s *Service) ListLocations(ctx context.Context, params ListLocationsParams) (ListLocationsResult, error) {
	var res ListLocationsResult
	p, err := access.Require(ctx, access.PermLocationsRead)
	if err != nil {
		return res, err
	}
	limit, err := httpx.Limit(params.Limit)
	if err != nil {
		return res, err
	}
	cur, err := httpx.DecodeCursor(params.Cursor)
	if err != nil {
		return res, err
	}

	err = s.DB.InTenantTx(ctx, p.Tenant(), func(tx *db.Tx) error {
		if params.UpdatedSince != nil {
			q := store.ListLocationsUpdatedSinceParams{
				UpdatedSince: *params.UpdatedSince, LocationIds: p.LocationIDs, RowLimit: limit + 1,
			}
			if cur != nil {
				t, err := httpx.ParseTimeKey(cur)
				if err != nil {
					return err
				}
				q.AfterUpdated, q.AfterID = t, &cur.ID
			}
			syncUntil := s.Now().Add(-httpx.SyncMargin).UTC()
			rows, err := tx.ListLocationsUpdatedSince(ctx, q)
			if err != nil {
				return err
			}
			var more bool
			res.Items, more = httpx.Page(rows, limit)
			res.SyncUntil = &syncUntil
			if more {
				last := res.Items[len(res.Items)-1]
				res.NextCursor = httpx.EncodeCursor(httpx.TimeKey(last.UpdatedAt), last.ID)
			}
			return nil
		}

		q := store.ListLocationsByNameParams{
			IncludeArchived: params.IncludeArchived, LocationIds: p.LocationIDs, RowLimit: limit + 1,
		}
		if cur != nil {
			q.AfterName, q.AfterID = &cur.Key, &cur.ID
		}
		rows, err := tx.ListLocationsByName(ctx, q)
		if err != nil {
			return err
		}
		var more bool
		res.Items, more = httpx.Page(rows, limit)
		if more {
			last := res.Items[len(res.Items)-1]
			res.NextCursor = httpx.EncodeCursor(last.Name, last.ID)
		}
		return nil
	})
	return res, err
}
