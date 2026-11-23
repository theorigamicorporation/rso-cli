// Package cli is the rso command: its commands, flags and what they print.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/theorigamicorporation/rso-cli/internal/config"
	"github.com/theorigamicorporation/rso-cli/internal/output"
	"github.com/theorigamicorporation/rsodev-go/platform"
)

// Env is everything the command reads from its surroundings, so tests can
// replace all of it.
type Env struct {
	In     io.Reader
	Out    io.Writer
	Err    io.Writer
	Getenv func(string) string
	// HTTP is used for every request; nil means a client with a timeout.
	HTTP *http.Client
	// Version is printed by `rso version`.
	Version string
	// OpenBrowser opens a URL for the person signing in; nil skips it.
	OpenBrowser func(url string) error
}

// OSEnv is the environment of a real run.
func OSEnv(version string) Env {
	return Env{In: os.Stdin, Out: os.Stdout, Err: os.Stderr, Getenv: os.Getenv, Version: version, OpenBrowser: openBrowser}
}

type app struct {
	env Env

	// Global flags.
	tenant string
	region string
	apiURL string
	format string
}

// New builds the rso command.
func New(env Env) *cobra.Command {
	if env.Getenv == nil {
		env.Getenv = func(string) string { return "" }
	}
	a := &app{env: env}
	root := &cobra.Command{
		Use:   "rso",
		Short: "Manage RSO cloud resources from the command line",
		Long: `rso manages the resources of an RSO cloud tenant: apps, instances,
PostgreSQL, object storage, registries, Kubernetes clusters, queues and
topics, scheduled jobs and messages, ingress, DNS and access.

Sign in once with "rso login" and pick a tenant with "rso tenant use". In
automation, set RSO_SERVICE_ACCOUNT_NAME and RSO_SERVICE_ACCOUNT_SECRET (or
RSO_ACCESS_TOKEN) and RSO_TENANT instead.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		// A wrong --output is refused before anything is asked of the API.
		PersistentPreRunE: func(*cobra.Command, []string) error {
			if _, err := output.ParseFormat(a.format); err != nil {
				return usageError{err}
			}
			return nil
		},
	}
	root.SetIn(env.In)
	root.SetOut(env.Out)
	root.SetErr(env.Err)

	flags := root.PersistentFlags()
	flags.StringVar(&a.tenant, "tenant", "", "tenant to act in (default: RSO_TENANT, then the tenant chosen with \"rso tenant use\")")
	flags.StringVar(&a.region, "region", "", "region of the tenant (default: RSO_REGION, then the chosen tenant's region)")
	flags.StringVar(&a.apiURL, "api", "", "API endpoint (default: RSO_API_URL, then "+platform.DefaultEndpoint+")")
	flags.StringVarP(&a.format, "output", "o", "table", "output format: table, json or yaml")

	root.AddCommand(
		a.loginCommand(), a.logoutCommand(), a.whoamiCommand(), a.tenantCommand(),
		a.queueCommand(), a.topicCommand(), a.webhookCommand(), a.scheduleCommand(), a.eventsCommand(),
		a.jobCommand(), a.bucketCommand(), a.kubernetesCommand(), a.appCommand(),
		a.instanceCommand(), a.postgresCommand(), a.registryCommand(), a.volumeCommand(), a.sshKeyCommand(),
		a.serviceCommand(), a.ingressCommand(), a.middlewareCommand(), a.dnsCommand(),
		a.serviceAccountCommand(), a.accessCommand(),
		a.quotaCommand(), a.aiCommand(), a.versionCommand(),
	)
	strictGroups(root)
	return root
}

// strictGroups makes "rso queue nonsense" an error. A command that only
// groups others prints its help when called alone, and cobra would do the
// same, successfully, for a subcommand that does not exist.
func strictGroups(cmd *cobra.Command) {
	for _, child := range cmd.Commands() {
		strictGroups(child)
	}
	if !cmd.HasSubCommands() || cmd.Run != nil || cmd.RunE != nil {
		return
	}
	cmd.Args = cobra.ArbitraryArgs
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return usagef("unknown command %q for %q: see \"%s --help\"", args[0], cmd.CommandPath(), cmd.CommandPath())
		}
		return cmd.Help()
	}
}

func (a *app) store() (config.Store, error) {
	dir, err := config.Dir(a.env.Getenv)
	if err != nil {
		return config.Store{}, err
	}
	return config.Store{Dir: dir}, nil
}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (a *app) httpClient() *http.Client {
	if a.env.HTTP != nil {
		return a.env.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (a *app) oauth(settings config.Settings) platform.OAuth {
	return platform.OAuth{
		// Whatever is not set here comes from the platform's configuration
		// document, then from the defaults.
		Endpoint:   first(a.apiURL, a.env.Getenv("RSO_API_URL"), settings.APIURL, platform.DefaultEndpoint),
		TokenURL:   first(a.env.Getenv("RSO_TOKEN_URL"), settings.TokenURL),
		DeviceURL:  a.env.Getenv("RSO_DEVICE_URL"),
		ClientID:   first(a.env.Getenv("RSO_CLIENT_ID"), settings.ClientID),
		HTTPClient: a.httpClient(),
	}
}

// tokens picks the credentials for this run: an access token or a service
// account from the environment first, then the saved sign-in.
func (a *app) tokens(store config.Store, settings config.Settings) (platform.TokenSource, error) {
	if token := a.env.Getenv("RSO_ACCESS_TOKEN"); token != "" {
		return platform.StaticToken(token), nil
	}
	name, secret := a.env.Getenv("RSO_SERVICE_ACCOUNT_NAME"), a.env.Getenv("RSO_SERVICE_ACCOUNT_SECRET")
	if name != "" && secret != "" {
		return &platform.ServiceAccountSource{OAuth: a.oauth(settings), Name: name, Secret: secret}, nil
	}
	set, err := store.Credentials()
	if err != nil {
		return nil, err
	}
	if set.AccessToken == "" {
		return nil, platform.ErrNotSignedIn
	}
	return &platform.RefreshingSource{OAuth: a.oauth(settings), Set: set, Save: store.SaveCredentials}, nil
}

// client builds the API client for a command. anyTenant is for calls that
// are about the caller and not about a tenant.
func (a *app) client(anyTenant bool) (*platform.Client, error) {
	store, err := a.store()
	if err != nil {
		return nil, err
	}
	settings, err := store.Settings()
	if err != nil {
		return nil, err
	}
	tokens, err := a.tokens(store, settings)
	if err != nil {
		return nil, err
	}
	tenant := first(a.tenant, a.env.Getenv("RSO_TENANT"), settings.Tenant)
	if tenant == "" && !anyTenant {
		return nil, errNoTenant
	}
	options := []platform.Option{
		platform.WithTokenSource(tokens),
		platform.WithUserAgent("rso-cli/" + a.env.Version),
		platform.WithHTTPClient(a.httpClient()),
	}
	if tenant != "" {
		options = append(options, platform.WithTenant(tenant))
	}
	if api := first(a.apiURL, a.env.Getenv("RSO_API_URL"), settings.APIURL); api != "" {
		options = append(options, platform.WithEndpoint(api))
	}
	region := first(a.region, a.env.Getenv("RSO_REGION"))
	if region == "" && tenant == settings.Tenant {
		// The saved region belongs to the saved tenant only.
		region = settings.Region
	}
	if region != "" {
		options = append(options, platform.WithRegion(region))
	}
	return platform.New(options...)
}

var errNoTenant = errors.New(`no tenant chosen: run "rso tenant use <tenant>", pass --tenant or set RSO_TENANT`)

// print writes a result in the format asked for with --output.
func (a *app) print(cmd *cobra.Command, view output.View) error {
	format, err := output.ParseFormat(a.format)
	if err != nil {
		return usageError{err}
	}
	return output.Write(cmd.OutOrStdout(), format, view)
}

// note tells the person something that is not the result; it goes to stderr
// so piping the result stays clean.
func (a *app) note(cmd *cobra.Command, format string, args ...any) {
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), format+"\n", args...)
}

// usageError marks a mistake in how the command was called.
type usageError struct{ error }

func (u usageError) Unwrap() error { return u.error }

func usagef(format string, args ...any) error {
	return usageError{fmt.Errorf(format, args...)}
}

// Exit codes. Scripts can tell a missing object from a refused one.
const (
	ExitOK          = 0
	ExitError       = 1
	ExitUsage       = 2
	ExitAuth        = 3
	ExitNotFound    = 4
	ExitForbidden   = 5
	ExitConflict    = 6
	ExitQuota       = 7
	ExitUnavailable = 8
)

// ExitCode maps an error to the exit code of the process.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var usage usageError
	if errors.As(err, &usage) || errors.Is(err, errNoTenant) {
		return ExitUsage
	}
	switch platform.KindOf(err) {
	case platform.KindUnauthenticated:
		return ExitAuth
	case platform.KindNotFound:
		return ExitNotFound
	case platform.KindForbidden:
		return ExitForbidden
	case platform.KindConflict:
		return ExitConflict
	case platform.KindQuota:
		return ExitQuota
	case platform.KindUnavailable:
		return ExitUnavailable
	case platform.KindInvalid:
		return ExitUsage
	}
	var refused *platform.OAuthError
	if errors.As(err, &refused) {
		return ExitAuth
	}
	return ExitError
}

// Message is what the person is told about an error.
func Message(err error) string {
	var api *platform.Error
	var data *platform.DataError
	switch {
	case errors.As(err, &api):
		if len(api.Errors) == 0 {
			return api.Message
		}
		parts := make([]string, 0, len(api.Errors))
		for _, e := range api.Errors {
			parts = append(parts, e.Message)
		}
		return strings.Join(parts, "; ")
	case errors.As(err, &data):
		return data.Message
	case errors.Is(err, platform.ErrSessionExpired):
		return `your session has expired: run "rso login"`
	case errors.Is(err, platform.ErrNotSignedIn):
		return `not signed in: run "rso login", or set RSO_SERVICE_ACCOUNT_NAME and RSO_SERVICE_ACCOUNT_SECRET`
	}
	return err.Error()
}

// Run executes the command line and returns the process exit code.
func Run(ctx context.Context, env Env, args []string) int {
	root := New(env)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return ExitOK
	}
	_, _ = fmt.Fprintln(env.Err, "rso:", Message(err))
	code := ExitCode(err)
	// cobra's own complaints (unknown command, bad flag) are usage errors.
	if code == ExitError && isCobraUsage(err) {
		return ExitUsage
	}
	return code
}
