package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/theorigamicorporation/rso-cli/internal/output"
	"github.com/theorigamicorporation/rsodev-go/platform"
)

var serviceColumns = []string{"NAME", "TARGET", "PORTS"}

func serviceRow(s platform.ExternalService) []string {
	ports := make([]string, 0, len(s.Ports))
	for _, p := range s.Ports {
		ports = append(ports, fmt.Sprintf("%d>%d", p.Port, p.TargetPort))
	}
	return []string{s.Name, s.ExternalName, strings.Join(ports, ",")}
}

// servicePort parses "80" or "80:8080" (port, then the target's port).
func servicePort(text string) (platform.ServicePort, error) {
	left, right, split := strings.Cut(text, ":")
	port, err := strconv.Atoi(left)
	target := port
	if err == nil && split {
		target, err = strconv.Atoi(right)
	}
	if err != nil || port <= 0 || target <= 0 {
		return platform.ServicePort{}, usagef("--port takes a port or port:target-port, got %q", text)
	}
	return platform.ServicePort{Port: port, TargetPort: target, Protocol: "TCP"}, nil
}

func (a *app) serviceCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "service", Aliases: []string{"services"}, Short: "Name things ingress routes can send traffic to"}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List external services",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, _ []string) error {
			services, err := client.ListExternalServices(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(services))
			for _, s := range services {
				rows = append(rows, serviceRow(s))
			}
			return a.print(cmd, output.View{Data: services, Columns: serviceColumns, Rows: rows, Empty: "No external services yet."})
		}),
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get <service>",
		Short: "Show an external service",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			s, err := client.GetExternalService(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: s, Columns: serviceColumns, Rows: [][]string{serviceRow(*s)}})
		}),
	})

	var target string
	var ports []string
	create := &cobra.Command{
		Use:   "create <service> --target <host> --port <port>",
		Short: "Create an external service",
		Long: `Give a name to something outside the app platform, such as an instance,
so that ingress routes can send traffic to it.

  rso service create shop --target shop.hosts.example.com --port 80:8080`,
		Args: cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			if target == "" || len(ports) == 0 {
				return usagef("--target and --port are required")
			}
			in := platform.ExternalService{Name: args[0], ExternalName: target}
			for _, text := range ports {
				port, err := servicePort(text)
				if err != nil {
					return err
				}
				in.Ports = append(in.Ports, port)
			}
			s, err := client.CreateExternalService(cmd.Context(), in)
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: s, Columns: serviceColumns, Rows: [][]string{serviceRow(*s)}})
		}),
	}
	create.Flags().StringVar(&target, "target", "", "host name or address the service stands for")
	create.Flags().StringArrayVar(&ports, "port", nil, "port, or port:target-port (repeatable)")
	cmd.AddCommand(create)

	cmd.AddCommand(a.deleteCommand("delete <service>", "Delete an external service",
		"Delete external service %s?", "Deleted external service %s.",
		a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			return client.DeleteExternalService(cmd.Context(), args[0])
		})))
	return cmd
}

var ingressColumns = []string{"NAME", "RULES", "SERVICES", "MIDDLEWARES", "TLS"}

func ingressRow(r platform.IngressRoute) []string {
	var rules, services, middlewares []string
	for _, rule := range r.Routes {
		rules = append(rules, rule.Match)
		for _, s := range rule.Services {
			services = append(services, fmt.Sprintf("%s:%d", s.Name, s.Port))
		}
		middlewares = append(middlewares, rule.Middlewares...)
	}
	return []string{r.Name, strings.Join(rules, " | "), strings.Join(services, ","), strings.Join(middlewares, ","), yesNo(deref(r.CertResolver) != "")}
}

// routeService parses "name:port".
func routeService(text string) (platform.RouteService, error) {
	name, right, split := strings.Cut(text, ":")
	port, err := strconv.Atoi(right)
	if !split || name == "" || err != nil || port <= 0 {
		return platform.RouteService{}, usagef("--service takes name:port, got %q", text)
	}
	return platform.RouteService{Name: name, Port: port}, nil
}

func (a *app) ingressCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "ingress", Aliases: []string{"route", "routes"}, Short: "Publish services under hostnames"}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List ingress routes",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, _ []string) error {
			routes, err := client.ListIngressRoutes(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(routes))
			for _, r := range routes {
				rows = append(rows, ingressRow(r))
			}
			return a.print(cmd, output.View{Data: routes, Columns: ingressColumns, Rows: rows, Empty: "No ingress routes yet."})
		}),
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get <route>",
		Short: "Show an ingress route",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			r, err := client.GetIngressRoute(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: r, Columns: ingressColumns, Rows: [][]string{ingressRow(*r)}})
		}),
	})

	var host, pathPrefix, match string
	var services, middlewares []string
	create := &cobra.Command{
		Use:   "create <route> --host <hostname> --service <service>:<port>",
		Short: "Create an ingress route",
		Long: `Publish an external service (see "rso service") under a hostname.

  rso ingress create shop --host shop.example.com --service shop:80
  rso ingress create api --host example.com --path-prefix /api --service api:8080 --middleware strip-api

--match takes a whole rule instead of --host and --path-prefix, for rules
those two cannot express. A certificate is obtained for the hostname.`,
		Args: cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			if (match == "") == (host == "") {
				return usagef("give --host (with an optional --path-prefix) or --match")
			}
			if match != "" && pathPrefix != "" {
				return usagef("--path-prefix goes with --host, not with --match")
			}
			if len(services) == 0 {
				return usagef("--service is required")
			}
			rule := platform.RouteRule{Match: match, Middlewares: middlewares}
			if host != "" {
				if strings.ContainsAny(host+pathPrefix, "`") {
					return usagef("a hostname or a path cannot contain a backquote")
				}
				rule.Match = "Host(`" + host + "`)"
				if pathPrefix != "" {
					rule.Match += " && PathPrefix(`" + pathPrefix + "`)"
				}
			}
			for _, text := range services {
				service, err := routeService(text)
				if err != nil {
					return err
				}
				rule.Services = append(rule.Services, service)
			}
			r, err := client.CreateIngressRoute(cmd.Context(), platform.IngressRoute{Name: args[0], Routes: []platform.RouteRule{rule}})
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: r, Columns: ingressColumns, Rows: [][]string{ingressRow(*r)}})
		}),
	}
	create.Flags().StringVar(&host, "host", "", "hostname to publish under")
	create.Flags().StringVar(&pathPrefix, "path-prefix", "", "with --host: only paths that start with this")
	create.Flags().StringVar(&match, "match", "", "a whole matching rule, instead of --host")
	create.Flags().StringArrayVar(&services, "service", nil, "external service and port to send traffic to, as name:port (repeatable)")
	create.Flags().StringArrayVar(&middlewares, "middleware", nil, "middleware to apply, in order (repeatable)")
	cmd.AddCommand(create)

	cmd.AddCommand(a.deleteCommand("delete <route>", "Delete an ingress route",
		"Delete ingress route %s? Its hostname stops answering.", "Deleted ingress route %s.",
		a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			return client.DeleteIngressRoute(cmd.Context(), args[0])
		})))
	return cmd
}

var middlewareColumns = []string{"NAME", "TYPE", "SETTINGS"}

func middlewareRow(m platform.Middleware) []string {
	settings := ""
	switch {
	case m.BasicAuth != nil:
		settings = "user " + m.BasicAuth.Username
	case m.StripPrefix != nil:
		settings = strings.Join(m.StripPrefix.Prefixes, ",")
	case m.RedirectScheme != nil:
		settings = "to " + m.RedirectScheme.Scheme
	case m.RedirectRegex != nil:
		settings = m.RedirectRegex.Regex + " -> " + m.RedirectRegex.Replacement
	}
	return []string{m.Name, m.Type, settings}
}

func (a *app) middlewareCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "middleware", Aliases: []string{"middlewares"}, Short: "Change requests on their way through an ingress route"}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List middlewares",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, _ []string) error {
			items, err := client.ListMiddlewares(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(items))
			for _, m := range items {
				rows = append(rows, middlewareRow(m))
			}
			return a.print(cmd, output.View{Data: items, Columns: middlewareColumns, Rows: rows, Empty: "No middlewares yet."})
		}),
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get <middleware>",
		Short: "Show a middleware",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			m, err := client.GetMiddleware(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: m, Columns: middlewareColumns, Rows: [][]string{middlewareRow(*m)}})
		}),
	})

	var user, scheme, regex, replacement string
	var prefixes []string
	var permanent bool
	create := &cobra.Command{
		Use:   "create <middleware> (--basic-auth-user <user> | --strip-prefix <prefix> | --redirect-scheme <scheme> | --redirect-regex <regex> --replacement <url>)",
		Short: "Create a middleware",
		Long: `Create a middleware of one kind:

  --basic-auth-user    ask for a user name and a password; the password is
                       read from standard input, never from the command line
  --strip-prefix       remove a prefix from the path before forwarding
  --redirect-scheme    redirect to another scheme, usually https
  --redirect-regex     redirect URLs that match to --replacement

  printf '%s' "$PASSWORD" | rso middleware create staff --basic-auth-user ada
  rso middleware create strip-api --strip-prefix /api`,
		Args: cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			chosen := 0
			for _, given := range []bool{user != "", len(prefixes) > 0, scheme != "", regex != ""} {
				if given {
					chosen++
				}
			}
			if chosen != 1 {
				return usagef("give exactly one of --basic-auth-user, --strip-prefix, --redirect-scheme and --redirect-regex")
			}
			in := platform.Middleware{Name: args[0]}
			switch {
			case user != "":
				password, err := a.readAll()
				if err != nil {
					return err
				}
				if password = strings.TrimRight(password, "\r\n"); password == "" {
					return usagef("no password on standard input")
				}
				in.Type, in.BasicAuth = platform.MiddlewareBasicAuth, &platform.BasicAuth{Username: user, Password: password}
			case len(prefixes) > 0:
				in.Type, in.StripPrefix = platform.MiddlewareStripPrefix, &platform.StripPrefix{Prefixes: prefixes}
			case scheme != "":
				in.Type, in.RedirectScheme = platform.MiddlewareRedirectScheme, &platform.RedirectScheme{Scheme: scheme, Permanent: permanent}
			default:
				if replacement == "" {
					return usagef("--redirect-regex needs --replacement")
				}
				in.Type, in.RedirectRegex = platform.MiddlewareRedirectRegex, &platform.RedirectRegex{Regex: regex, Replacement: replacement, Permanent: permanent}
			}
			m, err := client.CreateMiddleware(cmd.Context(), in)
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: m, Columns: middlewareColumns, Rows: [][]string{middlewareRow(*m)}})
		}),
	}
	create.Flags().StringVar(&user, "basic-auth-user", "", "ask for this user name; the password comes from standard input")
	create.Flags().StringSliceVar(&prefixes, "strip-prefix", nil, "path prefix to remove (repeatable)")
	create.Flags().StringVar(&scheme, "redirect-scheme", "", "scheme to redirect to, usually https")
	create.Flags().StringVar(&regex, "redirect-regex", "", "redirect URLs that match this expression")
	create.Flags().StringVar(&replacement, "replacement", "", "with --redirect-regex: where to redirect to")
	create.Flags().BoolVar(&permanent, "permanent", false, "with a redirect: answer 301 instead of 302")
	cmd.AddCommand(create)

	cmd.AddCommand(a.deleteCommand("delete <middleware>", "Delete a middleware",
		"Delete middleware %s?", "Deleted middleware %s.",
		a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			return client.DeleteMiddleware(cmd.Context(), args[0])
		})))
	return cmd
}
