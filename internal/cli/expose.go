package cli

import (
	"net"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/theorigamicorporation/rso-cli/internal/output"
	"github.com/theorigamicorporation/rsodev-go/platform"
)

const previewNote = "\n\nPreview: this is not switched on everywhere yet. Where it is not, the\ncommand answers that the feature is not configured."

var exposureColumns = []string{"HOST", "STATUS", "TARGET", "TLS", "DNS", "UPDATED"}

func exposureTarget(e platform.Exposure) string {
	if e.Target == nil {
		return "(platform)"
	}
	return strings.ToLower(e.Target.Scheme) + "://" + net.JoinHostPort(e.Target.Address, strconv.Itoa(e.Target.Port))
}

func exposureDNS(e platform.Exposure) string {
	if e.DNSManaged {
		return "managed"
	}
	return strings.Join(e.DNSTarget, ", ")
}

func exposureRow(e platform.Exposure) []string {
	return []string{e.Host, e.Status, exposureTarget(e), strings.ToLower(e.TLS), exposureDNS(e), when(e.UpdatedAt)}
}

func (a *app) printExposure(cmd *cobra.Command, e *platform.Exposure) error {
	fields := [][2]string{
		{"Host", e.Host}, {"Namespace", e.Namespace}, {"Status", e.Status}, {"Target", exposureTarget(*e)}, {"TLS", strings.ToLower(e.TLS)},
		{"DNS managed", yesNo(e.DNSManaged)},
	}
	if len(e.DNSTarget) > 0 {
		fields = append(fields, [2]string{"Point the host at", strings.Join(e.DNSTarget, ", ")})
	}
	if e.Domain != nil {
		verified := ""
		if e.DomainVerified != nil {
			verified = yesNo(*e.DomainVerified)
		}
		fields = append(fields, [2]string{"Domain", *e.Domain}, [2]string{"Domain verified", verified})
	}
	if e.Target != nil && e.Target.Scheme == platform.ExposureHTTPS && e.Target.InsecureSkipVerify != nil {
		checked := "checked"
		if *e.Target.InsecureSkipVerify {
			checked = "not checked"
		}
		fields = append(fields, [2]string{"Target certificate", checked})
	}
	if e.Proxied != nil {
		fields = append(fields, [2]string{"DNS proxied", yesNo(*e.Proxied)})
	}
	if e.TargetOutOfRange {
		fields = append(fields, [2]string{"Target out of range", "yes: publish the host again with its new address"})
	}
	fields = append(fields,
		[2]string{"Sign-in user", deref(e.BasicAuthUser)}, [2]string{"Managed by platform", yesNo(e.ManagedByPlatform)},
		[2]string{"Created", when(e.CreatedAt)}, [2]string{"Created by", deref(e.CreatedBy)}, [2]string{"Updated", when(e.UpdatedAt)})
	return a.print(cmd, output.View{Data: e, Fields: fields})
}

// exposeTarget parses --to: an address and a port, with an optional scheme
// in front.
func exposeTarget(to, scheme string) (platform.ExposureTarget, error) {
	rest := to
	if before, after, found := strings.Cut(to, "://"); found {
		if scheme != "" && !strings.EqualFold(scheme, before) {
			return platform.ExposureTarget{}, usagef("--to says %s and --scheme says %s", before, scheme)
		}
		scheme, rest = before, after
	}
	switch strings.ToUpper(scheme) {
	case "", platform.ExposureHTTP:
		scheme = platform.ExposureHTTP
	case platform.ExposureHTTPS:
		scheme = platform.ExposureHTTPS
	default:
		return platform.ExposureTarget{}, usagef("the scheme is http or https, got %q", scheme)
	}
	address, portText, err := net.SplitHostPort(rest)
	if err != nil || address == "" {
		return platform.ExposureTarget{}, usagef("--to takes <address>:<port>, got %q", to)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return platform.ExposureTarget{}, usagef("--to takes a port from 1 to 65535, got %q", portText)
	}
	return platform.ExposureTarget{Address: address, Port: port, Scheme: scheme}, nil
}

func (a *app) exposeCommand() *cobra.Command {
	var to, scheme, tls, user, namespace string
	var noAuth, skipVerify bool
	cmd := &cobra.Command{
		Use:   "expose <hostname> --to <address>:<port>",
		Short: "Publish a hostname and send its traffic to an address in the tenant's network (preview)",
		Long: `Publish a hostname: the platform routes it to the target, obtains its
certificate and, in a domain it manages, keeps its DNS record. Running the
command again changes where the host goes.

  rso expose shop.example.com --to 198.51.100.10:8080
  rso expose shop.example.com --to https://198.51.100.10:8443 --insecure-skip-verify

The target is an address in the tenant's own private network, such as its
load balancer. The hostname is under the tenant's platform domain or under a
domain of your own that is verified (see "rso domain").

  --insecure-skip-verify
                       needed the first time a host is published to an https
                       target: the platform then does not check the
                       certificate the target presents. A target is an
                       address, which a certificate rarely names. Give
                       --insecure-skip-verify=false to have it checked. Left
                       out for a host that exists, the host keeps its setting.

  --basic-auth-user    visitors sign in with this user name and a password;
                       the password is read from standard input:

  printf '%s' "$PASSWORD" | rso expose shop.example.com --to 198.51.100.10:8080 --basic-auth-user ada` + previewNote,
		Args: cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			if to == "" {
				return usagef("--to is required: the <address>:<port> the host's traffic goes to")
			}
			target, err := exposeTarget(to, scheme)
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("insecure-skip-verify") {
				if target.Scheme != platform.ExposureHTTPS {
					return usagef("--insecure-skip-verify is for an https target")
				}
				target.InsecureSkipVerify = &skipVerify
			}
			in := platform.ExposeHostInput{Host: strings.ToLower(args[0]), Target: target, Namespace: namespace}
			switch strings.ToUpper(tls) {
			case "":
			case platform.ExposureTLSAuto:
				in.TLS = platform.ExposureTLSAuto
			case platform.ExposureTLSNone:
				in.TLS = platform.ExposureTLSNone
			default:
				return usagef("--tls is auto or none, got %q", tls)
			}
			if user != "" && noAuth {
				return usagef("give at most one of --basic-auth-user and --no-basic-auth")
			}
			if user != "" {
				password, err := a.readAll()
				if err != nil {
					return err
				}
				if password = strings.TrimRight(password, "\r\n"); password == "" {
					return usagef("no password on standard input")
				}
				in.BasicAuth = &platform.ExposureBasicAuth{Username: user, Password: password}
			}
			in.RemoveBasicAuth = noAuth
			e, err := client.ExposeHost(cmd.Context(), in)
			if err != nil {
				return err
			}
			if !e.DNSManaged && len(e.DNSTarget) > 0 {
				a.note(cmd, "Point %s at %s in the DNS of its domain.", e.Host, strings.Join(e.DNSTarget, ", "))
			}
			return a.print(cmd, output.View{Data: e, Columns: exposureColumns, Rows: [][]string{exposureRow(*e)}})
		}),
	}
	cmd.Flags().StringVar(&to, "to", "", "where the traffic goes: <address>:<port>, optionally with http:// or https:// in front")
	cmd.Flags().StringVar(&scheme, "scheme", "", "how the platform talks to the target: http (default) or https")
	cmd.Flags().BoolVar(&skipVerify, "insecure-skip-verify", false, "https targets: do not check the certificate the target presents")
	cmd.Flags().StringVar(&tls, "tls", "", "auto: the platform obtains a certificate (default); none: plain HTTP")
	cmd.Flags().StringVar(&user, "basic-auth-user", "", "visitors sign in with this user name; the password comes from standard input")
	cmd.Flags().BoolVar(&noAuth, "no-basic-auth", false, "remove the sign-in the host has")
	cmd.Flags().StringVar(&namespace, "namespace", "", "platform administrators: the namespace the host is for")
	return cmd
}

func (a *app) exposureCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "exposure", Aliases: []string{"exposures"}, Short: "Published hostnames (preview)",
		Long: `The hostnames published with "rso expose".` + previewNote}

	var listNamespace string
	list := &cobra.Command{
		Use:   "list",
		Short: "List published hostnames",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, _ []string) error {
			exposures, err := client.ListExposures(cmd.Context(), listNamespace)
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(exposures))
			for _, e := range exposures {
				rows = append(rows, exposureRow(e))
			}
			return a.print(cmd, output.View{Data: exposures, Columns: exposureColumns, Rows: rows, Empty: "No hostnames are published yet."})
		}),
	}
	list.Flags().StringVar(&listNamespace, "namespace", "", "platform administrators: only this namespace")
	cmd.AddCommand(list)

	var getNamespace string
	get := &cobra.Command{
		Use:   "get <hostname>",
		Short: "Show a published hostname",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			e, err := client.GetExposure(cmd.Context(), strings.ToLower(args[0]), getNamespace)
			if err != nil {
				return err
			}
			return a.printExposure(cmd, e)
		}),
	}
	get.Flags().StringVar(&getNamespace, "namespace", "", "platform administrators: the namespace that holds the host")
	cmd.AddCommand(get)

	var removeNamespace string
	remove := a.deleteCommand("remove <hostname>", "Withdraw a published hostname",
		"Withdraw %s? It stops answering.", "Withdrew %s.",
		a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			return client.UnexposeHost(cmd.Context(), strings.ToLower(args[0]), removeNamespace)
		}))
	remove.Aliases = []string{"delete"}
	remove.Flags().StringVar(&removeNamespace, "namespace", "", "platform administrators: the namespace that holds the host")
	cmd.AddCommand(remove)
	return cmd
}

var domainColumns = []string{"DOMAIN", "STATUS", "VERIFIED", "CHECKED"}

func domainRow(d platform.Domain) []string {
	return []string{d.Domain, d.Status, when(deref(d.VerifiedAt)), when(deref(d.LastCheckedAt))}
}

func (a *app) printDomain(cmd *cobra.Command, d *platform.Domain) error {
	return a.print(cmd, output.View{Data: d, Fields: [][2]string{
		{"Domain", d.Domain}, {"Status", d.Status}, {"TXT record name", d.ChallengeName}, {"TXT record value", d.ChallengeValue},
		{"Verified", when(deref(d.VerifiedAt))}, {"Last checked", when(deref(d.LastCheckedAt))}, {"Created", when(d.CreatedAt)},
	}})
}

func (a *app) domainCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "domain", Aliases: []string{"domains"}, Short: "Domains of your own to publish hostnames under (preview)",
		Long: `Prove that a domain is yours, so hostnames at and below it can be
published with "rso expose":

  rso domain add example.com        names a TXT record to publish
  rso domain verify example.com     once the record is in your DNS

Keep the TXT record: it is checked again from time to time.` + previewNote}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the tenant's domains",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, _ []string) error {
			domains, err := client.ListDomains(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(domains))
			for _, d := range domains {
				rows = append(rows, domainRow(d))
			}
			return a.print(cmd, output.View{Data: domains, Columns: domainColumns, Rows: rows, Empty: "No domains are registered yet."})
		}),
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "add <domain>",
		Short: "Register a domain and show the TXT record that proves it",
		Long:  "Register a domain, or show its TXT record again. Publish the record in the\ndomain's DNS, then run \"rso domain verify\".",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			d, err := client.RegisterDomain(cmd.Context(), strings.ToLower(args[0]))
			if err != nil {
				return err
			}
			if d.Status != platform.DomainVerified {
				a.note(cmd, "Publish the TXT record below, then run: rso domain verify %s", d.Domain)
			}
			return a.printDomain(cmd, d)
		}),
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "verify <domain>",
		Short: "Look for the domain's TXT record and mark the domain verified",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			d, err := client.VerifyDomain(cmd.Context(), strings.ToLower(args[0]))
			if err != nil {
				return err
			}
			return a.printDomain(cmd, d)
		}),
	})

	remove := a.deleteCommand("remove <domain>", "Give a domain up",
		"Remove the domain %s?", "Removed the domain %s.",
		a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			return client.RemoveDomain(cmd.Context(), strings.ToLower(args[0]))
		}))
	remove.Aliases = []string{"delete"}
	remove.Long = "Give a domain up. Refused while hostnames are published under it."
	cmd.AddCommand(remove)
	return cmd
}
