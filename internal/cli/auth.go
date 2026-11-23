package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/theorigamicorporation/rso-cli/internal/output"
	"github.com/theorigamicorporation/rsodev-go/platform"
)

func (a *app) loginCommand() *cobra.Command {
	var withToken, noBrowser bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in to the RSO cloud",
		Long: `Sign in with your own account. The command prints a link and a code; open
the link in a browser, confirm the code, and the sign-in is saved for later
commands.

With --with-token the access token is read from standard input instead, for
places with no browser:

  printf '%s' "$TOKEN" | rso login --with-token

Automation does not need to log in at all: set RSO_SERVICE_ACCOUNT_NAME and
RSO_SERVICE_ACCOUNT_SECRET, or RSO_ACCESS_TOKEN.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := a.store()
			if err != nil {
				return err
			}
			settings, err := store.Settings()
			if err != nil {
				return err
			}
			var set platform.TokenSet
			if withToken {
				raw, err := a.readAll()
				if err != nil {
					return err
				}
				set.AccessToken = strings.TrimSpace(raw)
				if set.AccessToken == "" {
					return usagef("no token on standard input")
				}
			} else {
				oauth := a.oauth(settings)
				code, err := oauth.StartDevice(cmd.Context())
				if err != nil {
					return signInHint(err)
				}
				link := first(code.VerificationURIComplete, code.VerificationURI)
				a.note(cmd, "Open this link to sign in:\n\n  %s\n", link)
				if code.VerificationURIComplete == "" {
					a.note(cmd, "and enter the code %s\n", code.UserCode)
				}
				if !noBrowser && a.env.OpenBrowser != nil {
					_ = a.env.OpenBrowser(link)
				}
				a.note(cmd, "Waiting for the sign-in to be confirmed...")
				if set, err = oauth.WaitDevice(cmd.Context(), code); err != nil {
					return signInHint(err)
				}
			}

			// Prove the token works before it replaces a saved one.
			options := []platform.Option{
				platform.WithAccessToken(set.AccessToken),
				platform.WithUserAgent("rso-cli/" + a.env.Version),
				platform.WithHTTPClient(a.httpClient()),
			}
			if api := first(a.apiURL, a.env.Getenv("RSO_API_URL"), settings.APIURL); api != "" {
				options = append(options, platform.WithEndpoint(api))
			}
			client, err := platform.New(options...)
			if err != nil {
				return err
			}
			me, err := client.Me(cmd.Context())
			if err != nil {
				return err
			}
			if err := store.SaveCredentials(set); err != nil {
				return err
			}
			a.note(cmd, "Signed in as %s.", first(me.Email, me.Username))

			// One tenant: nothing to choose. More: keep a valid earlier choice.
			if chosen := pickTenant(me.Tenants, settings.Tenant); chosen != nil {
				settings.Tenant, settings.TenantName, settings.Region = chosen.ID, chosen.Name, chosen.Region
				if err := store.SaveSettings(settings); err != nil {
					return err
				}
				a.note(cmd, "Tenant: %s.", tenantLabel(*chosen))
			} else if len(me.Tenants) > 1 {
				a.note(cmd, `You belong to %d tenants: pick one with "rso tenant use <tenant>".`, len(me.Tenants))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&withToken, "with-token", false, "read an access token from standard input instead of signing in with a browser")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "print the link only, do not try to open it")
	return cmd
}

// signInHint explains a sign-in the platform does not accept from this
// program, which a person cannot fix by trying again.
func signInHint(err error) error {
	var refused *platform.OAuthError
	if errors.As(err, &refused) && (refused.Code == "invalid_client" || refused.Code == "unauthorized_client") {
		return fmt.Errorf("%w\nsigning in from the command line is not enabled for this client. "+
			"Use a service account (RSO_SERVICE_ACCOUNT_NAME and RSO_SERVICE_ACCOUNT_SECRET), "+
			"pass an access token with \"rso login --with-token\", or set RSO_CLIENT_ID to a client that allows it", err)
	}
	return err
}

// pickTenant returns the tenant to use after a sign-in: the saved one if the
// account still belongs to it, or the only one there is.
func pickTenant(tenants []platform.TenantMembership, saved string) *platform.TenantMembership {
	for i := range tenants {
		if saved != "" && tenants[i].ID == saved {
			return &tenants[i]
		}
	}
	if len(tenants) == 1 {
		return &tenants[0]
	}
	return nil
}

func tenantLabel(tenant platform.TenantMembership) string {
	if tenant.Name != "" && tenant.Name != tenant.ID {
		return tenant.Name + " (" + tenant.ID + ")"
	}
	return tenant.ID
}

func (a *app) logoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Forget the saved sign-in",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := a.store()
			if err != nil {
				return err
			}
			if err := store.DeleteCredentials(); err != nil {
				return err
			}
			a.note(cmd, "Signed out.")
			return nil
		},
	}
}

func (a *app) whoamiCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the signed-in account and the tenant in use",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.client(true)
			if err != nil {
				return err
			}
			me, err := client.Me(cmd.Context())
			if err != nil {
				return err
			}
			current := client.Tenant()
			label := current
			for _, tenant := range me.Tenants {
				if tenant.ID == current {
					label = tenantLabel(tenant)
				}
			}
			return a.print(cmd, output.View{
				Data: map[string]any{"account": me, "tenant": current},
				Fields: [][2]string{
					{"Account", first(me.Email, me.Username)},
					{"Name", me.Name},
					{"Tenant", label},
					{"Tenants", strings.Join(tenantNames(me.Tenants), ", ")},
				},
			})
		},
	}
}

func tenantNames(tenants []platform.TenantMembership) []string {
	names := make([]string, 0, len(tenants))
	for _, tenant := range tenants {
		names = append(names, first(tenant.Name, tenant.ID))
	}
	return names
}

func (a *app) tenantCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "tenant", Short: "Choose the tenant commands act in"}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List the tenants you belong to",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				client, err := a.client(true)
				if err != nil {
					return err
				}
				me, err := client.Me(cmd.Context())
				if err != nil {
					return err
				}
				rows := make([][]string, 0, len(me.Tenants))
				for _, tenant := range me.Tenants {
					marker := ""
					if tenant.ID == client.Tenant() {
						marker = "*"
					}
					rows = append(rows, []string{marker, tenant.Name, tenant.ID, tenant.Region, strings.Join(tenant.Roles, ",")})
				}
				return a.print(cmd, output.View{Data: me.Tenants, Columns: []string{"", "NAME", "ID", "REGION", "ROLES"}, Rows: rows, Empty: "You do not belong to a tenant yet."})
			},
		},
		&cobra.Command{
			Use:   "use <tenant>",
			Short: "Use a tenant, by id or name, for later commands",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				client, err := a.client(true)
				if err != nil {
					return err
				}
				me, err := client.Me(cmd.Context())
				if err != nil {
					return err
				}
				chosen, err := findTenant(me.Tenants, args[0])
				if err != nil {
					return err
				}
				store, err := a.store()
				if err != nil {
					return err
				}
				settings, err := store.Settings()
				if err != nil {
					return err
				}
				settings.Tenant, settings.TenantName, settings.Region = chosen.ID, chosen.Name, chosen.Region
				if err := store.SaveSettings(settings); err != nil {
					return err
				}
				a.note(cmd, "Using tenant %s.", tenantLabel(*chosen))
				return nil
			},
		},
		&cobra.Command{
			Use:   "current",
			Short: "Show the tenant in use",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				store, err := a.store()
				if err != nil {
					return err
				}
				settings, err := store.Settings()
				if err != nil {
					return err
				}
				tenant := first(a.tenant, a.env.Getenv("RSO_TENANT"), settings.Tenant)
				if tenant == "" {
					return errNoTenant
				}
				name := ""
				if tenant == settings.Tenant {
					name = settings.TenantName
				}
				return a.print(cmd, output.View{
					Data:   map[string]string{"tenant": tenant, "name": name},
					Fields: [][2]string{{"Tenant", tenant}, {"Name", name}},
				})
			},
		},
	)
	return cmd
}

// findTenant matches by id first, then by name; a name shared by two tenants
// has to be told apart by id.
func findTenant(tenants []platform.TenantMembership, want string) (*platform.TenantMembership, error) {
	for i := range tenants {
		if tenants[i].ID == want {
			return &tenants[i], nil
		}
	}
	var matches []*platform.TenantMembership
	for i := range tenants {
		if strings.EqualFold(tenants[i].Name, want) {
			matches = append(matches, &tenants[i])
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return nil, usagef(`you do not belong to a tenant called %q: see "rso tenant list"`, want)
	}
	return nil, usagef("%d tenants are called %q: use the id", len(matches), want)
}
