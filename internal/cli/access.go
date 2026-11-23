package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/theorigamicorporation/rso-cli/internal/output"
	"github.com/theorigamicorporation/rsodev-go/platform"
)

var serviceAccountColumns = []string{"ID", "NAME", "STATUS", "ACTIVE", "EXPIRES"}

func serviceAccountRow(s platform.ServiceAccount) []string {
	return []string{s.ID, s.Name, s.Status, yesNo(s.Active), when(deref(s.ExpiresAt))}
}

// findServiceAccount takes a service account by its id or by its name.
func findServiceAccount(ctx context.Context, client *platform.Client, ref string) (*platform.ServiceAccount, error) {
	accounts, err := client.ListServiceAccounts(ctx)
	if err != nil {
		return nil, err
	}
	for i := range accounts {
		if accounts[i].ID == ref || accounts[i].Name == ref {
			return &accounts[i], nil
		}
	}
	return nil, notFoundf("Service account %s not found", ref)
}

func secretFields(s *platform.ServiceAccountSecret) [][2]string {
	return [][2]string{{"ID", s.ServiceAccount.ID}, {"Name", s.ServiceAccount.Name}, {"Secret", s.Secret}, {"Expires", when(deref(s.ServiceAccount.ExpiresAt))}}
}

func (a *app) serviceAccountCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "service-account", Aliases: []string{"service-accounts", "sa"}, Short: "Manage the accounts automation signs in with"}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List service accounts",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, _ []string) error {
			accounts, err := client.ListServiceAccounts(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(accounts))
			for _, s := range accounts {
				rows = append(rows, serviceAccountRow(s))
			}
			return a.print(cmd, output.View{Data: accounts, Columns: serviceAccountColumns, Rows: rows, Empty: "No service accounts yet."})
		}),
	})

	var days int
	create := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a service account; its secret is shown once",
		Long: `Create a service account for automation. The secret is printed once and
cannot be read again; "rso service-account rotate" issues a new one.

A new service account can do nothing until it is granted access: see
"rso access".`,
		Args: cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			if days <= 0 {
				return usagef("--expires-in-days must be above zero")
			}
			s, err := client.CreateServiceAccount(cmd.Context(), args[0], time.Now().Add(time.Duration(days)*24*time.Hour))
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: s, Fields: secretFields(s)})
		}),
	}
	create.Flags().IntVar(&days, "expires-in-days", 90, "how long the service account lasts")
	cmd.AddCommand(create)

	cmd.AddCommand(&cobra.Command{
		Use:   "rotate <service-account>",
		Short: "Issue a new secret; the old one stops working",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			account, err := findServiceAccount(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			s, err := client.RotateServiceAccountSecret(cmd.Context(), account.ID)
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: s, Fields: secretFields(s)})
		}),
	})

	active := func(use, short, done string, value bool) *cobra.Command {
		return &cobra.Command{
			Use:   use,
			Short: short,
			Args:  cobra.ExactArgs(1),
			RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
				account, err := findServiceAccount(cmd.Context(), client, args[0])
				if err != nil {
					return err
				}
				if _, err := client.SetServiceAccountActive(cmd.Context(), account.ID, value); err != nil {
					return err
				}
				a.note(cmd, done, account.Name)
				return nil
			}),
		}
	}
	cmd.AddCommand(
		active("enable <service-account>", "Let a service account sign in again", "Enabled service account %s.", true),
		active("disable <service-account>", "Stop a service account from signing in", "Disabled service account %s.", false),
	)

	cmd.AddCommand(a.deleteCommand("delete <service-account>", "Delete a service account",
		"Delete service account %s? Whatever signs in with it stops working.", "Deleted service account %s.",
		a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			account, err := findServiceAccount(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			return client.DeleteServiceAccount(cmd.Context(), account.ID)
		})))
	return cmd
}

var principalColumns = []string{"USERNAME", "TYPE", "ROLES", "PERMISSIONS", "SCOPED"}

func principalRow(p platform.Principal) []string {
	return []string{p.Username, p.Type, strings.Join(p.Roles, ","), strings.Join(p.Permissions, ","), fmt.Sprintf("%d", len(p.Scoped))}
}

func principalFields(p *platform.Principal) [][2]string {
	fields := [][2]string{
		{"Username", p.Username}, {"Name", deref(p.Name)}, {"Type", p.Type}, {"Restricted", yesNo(p.Restricted)},
		{"Roles", strings.Join(p.Roles, ", ")}, {"Permissions", strings.Join(p.Permissions, ", ")},
	}
	for _, scope := range p.Scoped {
		var on []string
		if len(scope.Selector.Names) > 0 {
			on = append(on, "names "+strings.Join(scope.Selector.Names, ","))
		}
		if len(scope.Selector.Prefixes) > 0 {
			on = append(on, "prefixes "+strings.Join(scope.Selector.Prefixes, ","))
		}
		for _, label := range scope.Selector.Labels {
			on = append(on, "label "+label.Key+"="+label.Value)
		}
		fields = append(fields, [2]string{"Scoped (" + scope.ResourceType + ")",
			strings.Join(append(append([]string{}, scope.Roles...), scope.Permissions...), ",") + " on " + strings.Join(on, "; ")})
	}
	return fields
}

func (a *app) accessCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "access", Short: "Decide who can do what in the tenant"}

	var permission string
	list := &cobra.Command{
		Use:   "list",
		Short: "List who has access to the tenant",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, _ []string) error {
			principals, err := client.ListPrincipals(cmd.Context(), permission)
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(principals))
			for _, p := range principals {
				rows = append(rows, principalRow(p))
			}
			return a.print(cmd, output.View{Data: principals, Columns: principalColumns, Rows: rows, Empty: "Nobody has access yet."})
		}),
	}
	list.Flags().StringVar(&permission, "permission", "", "only those who hold this permission")
	cmd.AddCommand(list)

	cmd.AddCommand(&cobra.Command{
		Use:   "get <username>",
		Short: "Show what a person or a service account may do",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			p, err := client.FindPrincipal(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if p == nil {
				return notFoundf("%s has no access to this tenant", args[0])
			}
			return a.print(cmd, output.View{Data: p, Fields: principalFields(p)})
		}),
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "roles",
		Short: "List the roles and the permissions they carry",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, _ []string) error {
			roles, err := client.ListRoles(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(roles))
			for _, r := range roles {
				rows = append(rows, []string{r.Name, yesNo(r.BuiltIn), r.Description, fmt.Sprintf("%d", len(r.Permissions))})
			}
			return a.print(cmd, output.View{Data: roles, Columns: []string{"NAME", "BUILT IN", "DESCRIPTION", "PERMISSIONS"}, Rows: rows, Empty: "No roles."})
		}),
	})

	change := func(use, short string, call func(ctx context.Context, client *platform.Client, username string, permissions []string) (*platform.Principal, error)) *cobra.Command {
		return &cobra.Command{
			Use:   use,
			Short: short,
			Args:  cobra.MinimumNArgs(2),
			RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
				p, err := call(cmd.Context(), client, args[0], args[1:])
				if err != nil {
					return err
				}
				return a.print(cmd, output.View{Data: p, Fields: principalFields(p)})
			}),
		}
	}
	cmd.AddCommand(
		change("grant <username> <permission>...", "Grant permissions on the whole tenant", func(ctx context.Context, client *platform.Client, username string, permissions []string) (*platform.Principal, error) {
			return client.GrantPermissions(ctx, username, permissions)
		}),
		change("revoke <username> <permission>...", "Take permissions away", func(ctx context.Context, client *platform.Client, username string, permissions []string) (*platform.Principal, error) {
			return client.RevokePermissions(ctx, username, permissions)
		}),
	)

	var resourceType string
	var names, prefixes, labels, roles, permissions []string
	var replace bool
	scope := &cobra.Command{
		Use:   "scope <username> --type <resource-type> (--name <name> | --prefix <prefix> | --label key=value) (--role <role> | --permission <permission>)",
		Short: "Grant access to some objects only",
		Long: `Grant roles or permissions on the objects of one type that a selector
picks: by name, by name prefix or by label.

  rso access scope ci-deployer --type bucket --name uploads --permission storage.objects.write
  rso access scope ada --type app --label team=shop --role EDITOR

The grant is added to the scoped grants the account already has; --replace
puts it in their place. "rso access resource-types" lists the types.`,
		Args: cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			if resourceType == "" {
				return usagef(`--type is required: see "rso access resource-types"`)
			}
			if len(names)+len(prefixes)+len(labels) == 0 {
				return usagef("give at least one of --name, --prefix and --label")
			}
			if len(roles)+len(permissions) == 0 {
				return usagef("give at least one of --role and --permission")
			}
			pairs, err := keyValues("label", labels)
			if err != nil {
				return err
			}
			grant := platform.ScopedAccess{ResourceType: resourceType, Roles: roles, Permissions: permissions,
				Selector: platform.Selector{Names: names, Prefixes: prefixes}}
			for _, label := range labels {
				key, _, _ := strings.Cut(label, "=")
				grant.Selector.Labels = append(grant.Selector.Labels, platform.KeyValue{Key: key, Value: pairs[key]})
			}
			scoped := []platform.ScopedAccess{}
			if !replace {
				current, err := client.FindPrincipal(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				if current != nil {
					scoped = append(scoped, current.Scoped...)
				}
			}
			scoped = append(scoped, grant)
			p, err := client.SetPrincipalAccess(cmd.Context(), platform.AccessInput{Username: args[0], Scoped: &scoped})
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: p, Fields: principalFields(p)})
		}),
	}
	scope.Flags().StringVar(&resourceType, "type", "", "type of the objects, for example bucket or app")
	scope.Flags().StringArrayVar(&names, "name", nil, "an object by its name (repeatable)")
	scope.Flags().StringArrayVar(&prefixes, "prefix", nil, "objects whose name starts with this (repeatable)")
	scope.Flags().StringArrayVar(&labels, "label", nil, "objects that carry this label, as key=value (repeatable)")
	scope.Flags().StringArrayVar(&roles, "role", nil, "role to grant on those objects (repeatable)")
	scope.Flags().StringArrayVar(&permissions, "permission", nil, "permission to grant on those objects (repeatable)")
	scope.Flags().BoolVar(&replace, "replace", false, "replace the scoped grants the account has instead of adding to them")
	cmd.AddCommand(scope)

	cmd.AddCommand(&cobra.Command{
		Use:   "resource-types",
		Short: "List the types of objects access can be scoped to",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, _ []string) error {
			types, err := client.ListResourceTypes(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(types))
			for _, t := range types {
				rows = append(rows, []string{t})
			}
			return a.print(cmd, output.View{Data: types, Columns: []string{"TYPE"}, Rows: rows, Empty: "No resource types."})
		}),
	})
	return cmd
}
