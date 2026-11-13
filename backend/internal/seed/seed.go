// Package seed creates demo data for local development. It is idempotent: fixed ids are
// used, and anything that already exists is left unchanged.
package seed

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"

	"dentalsaas/internal/access"
	"dentalsaas/internal/identity"
	"dentalsaas/internal/platform/db"
	"dentalsaas/internal/store"
)

const DemoPassword = "demo-password-123"

var (
	OrgID        = uuid.MustParse("01930000-0000-7000-8000-000000000001")
	SydneyID     = uuid.MustParse("01930000-0000-7000-8000-000000000101")
	ParramattaID = uuid.MustParse("01930000-0000-7000-8000-000000000102")
	OwnerID      = uuid.MustParse("01930000-0000-7000-8000-000000000201")
	FrontDeskID  = uuid.MustParse("01930000-0000-7000-8000-000000000202")
	ownerMemID   = uuid.MustParse("01930000-0000-7000-8000-000000000301")
	frontMemID   = uuid.MustParse("01930000-0000-7000-8000-000000000302")
)

type demoUser struct {
	id, membershipID uuid.UUID
	email, name      string
	role             access.Role
	locations        []uuid.UUID
}

var users = []demoUser{
	{OwnerID, ownerMemID, "owner@demo.local", "Olivia Owner", access.RoleOwner, nil},
	{FrontDeskID, frontMemID, "frontdesk@demo.local", "Felix Frontdesk", access.RoleFrontDesk, []uuid.UUID{ParramattaID}},
}

// Run seeds the demo organisation and prints what it did to out.
func Run(ctx context.Context, pool *db.Pool, out io.Writer) error {
	hash, err := identity.HashPassword(DemoPassword)
	if err != nil {
		return err
	}
	return pool.InTenantTx(ctx, db.Tenant{OrgID: OrgID}, func(tx *db.Tx) error {
		if _, err := tx.GetOrganisation(ctx, OrgID); db.IsNoRows(err) {
			abn := "51 824 753 556"
			if _, err := tx.CreateOrganisation(ctx, store.CreateOrganisationParams{ID: OrgID, Name: "Demo Dental", Abn: &abn}); err != nil {
				return err
			}
			fmt.Fprintln(out, "created organisation  Demo Dental")
		} else if err != nil {
			return err
		}

		for _, l := range []store.CreateLocationParams{
			{ID: SydneyID, OrgID: OrgID, Name: "Sydney CBD", AddressLine1: "1 George St", Suburb: "Sydney",
				State: "NSW", Postcode: "2000", Phone: "02 9000 0001", Timezone: "Australia/Sydney"},
			{ID: ParramattaID, OrgID: OrgID, Name: "Parramatta", AddressLine1: "10 Church St", Suburb: "Parramatta",
				State: "NSW", Postcode: "2150", Phone: "02 9000 0002", Timezone: "Australia/Sydney"},
		} {
			if _, err := tx.GetLocation(ctx, l.ID); db.IsNoRows(err) {
				if _, err := tx.CreateLocation(ctx, l); err != nil {
					return err
				}
				fmt.Fprintf(out, "created location      %s\n", l.Name)
			} else if err != nil {
				return err
			}
		}

		for _, u := range users {
			if err := tx.SetUser(ctx, u.id); err != nil {
				return err
			}
			if _, err := tx.GetUser(ctx, u.id); db.IsNoRows(err) {
				if err := tx.CreateUser(ctx, store.CreateUserParams{ID: u.id, Email: u.email, Name: u.name, PasswordHash: hash}); err != nil {
					return err
				}
				fmt.Fprintf(out, "created user          %-22s (%s)\n", u.email, u.role)
			} else if err != nil {
				return err
			}
			if _, err := tx.GetMembershipByUser(ctx, store.GetMembershipByUserParams{OrgID: OrgID, UserID: u.id}); db.IsNoRows(err) {
				if _, err := tx.UpsertMembership(ctx, store.UpsertMembershipParams{ID: u.membershipID, OrgID: OrgID, UserID: u.id, Role: string(u.role)}); err != nil {
					return err
				}
				for _, loc := range u.locations {
					if err := tx.AddMembershipLocation(ctx, store.AddMembershipLocationParams{OrgID: OrgID, MembershipID: u.membershipID, LocationID: loc}); err != nil {
						return err
					}
				}
			} else if err != nil {
				return err
			}
		}

		fmt.Fprintf(out, "\nDemo sign-ins (password %q):\n", DemoPassword)
		for _, u := range users {
			fmt.Fprintf(out, "  %-22s %s\n", u.email, u.role)
		}
		return nil
	})
}
