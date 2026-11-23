package cli

import (
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/theorigamicorporation/rso-cli/internal/output"
	"github.com/theorigamicorporation/rsodev-go/platform"
)

var appColumns = []string{"NAME", "URL", "REVISION", "UPDATED"}

func appRow(app platform.App) []string {
	return []string{app.Name, deref(app.Status.URL), deref(app.Status.LatestReadyRevision), when(deref(app.Status.LastUpdated))}
}

func appFields(app *platform.App) [][2]string {
	fields := [][2]string{
		{"Name", app.Name}, {"URL", deref(app.Status.URL)}, {"Revision", deref(app.Status.LatestReadyRevision)},
		{"Ready", yesNo(app.Ready())}, {"Updated", when(deref(app.Status.LastUpdated))},
	}
	for _, container := range app.Template.Spec.Containers {
		fields = append(fields, [2]string{"Image", container.Image})
		for _, port := range container.Ports {
			fields = append(fields, [2]string{"Port", strconv.Itoa(port.ContainerPort)})
		}
		names := make([]string, 0, len(container.Env))
		for _, env := range container.Env {
			names = append(names, env.Name)
		}
		if len(names) > 0 {
			fields = append(fields, [2]string{"Environment", strings.Join(names, ", ")})
		}
	}
	return fields
}

// appDeploy is what "rso app deploy" was asked to set.
type appDeploy struct {
	image       string
	port        int
	env         []string
	secretEnv   []string
	cpu, memory string
	pullSecrets []string
	wait        bool
	timeout     time.Duration
}

// apply puts the flags that were given on top of an app's description; what
// was not given stays as it is.
func (d appDeploy) apply(cmd *cobra.Command, template *platform.AppTemplate) error {
	if len(template.Spec.Containers) == 0 {
		template.Spec.Containers = []platform.AppContainer{{}}
	}
	container := &template.Spec.Containers[0]
	if d.image != "" {
		container.Image = d.image
	}
	if cmd.Flags().Changed("port") {
		container.Ports = []platform.ContainerPort{{ContainerPort: d.port}}
	}
	set := func(env platform.EnvVar) {
		for i := range container.Env {
			if container.Env[i].Name == env.Name {
				container.Env[i] = env
				return
			}
		}
		container.Env = append(container.Env, env)
	}
	values, err := keyValues("env", d.env)
	if err != nil {
		return err
	}
	for _, pair := range d.env {
		name, _, _ := strings.Cut(pair, "=")
		value := values[name]
		set(platform.EnvVar{Name: name, Value: &value})
	}
	for _, pair := range d.secretEnv {
		name, ref, found := strings.Cut(pair, "=")
		secret, key, split := strings.Cut(ref, ":")
		if !found || !split || name == "" || secret == "" || key == "" {
			return usagef("--secret-env takes NAME=secret:key, got %q", pair)
		}
		set(platform.EnvVar{Name: name, ValueFrom: &platform.EnvSource{SecretKeyRef: &platform.SecretKeyRef{Name: secret, Key: key}}})
	}
	if d.cpu != "" || d.memory != "" {
		if container.Resources == nil {
			container.Resources = &platform.Resources{}
		}
		if container.Resources.Limits == nil {
			container.Resources.Limits = &platform.ResourceList{}
		}
		if d.cpu != "" {
			container.Resources.Limits.CPU = &d.cpu
		}
		if d.memory != "" {
			container.Resources.Limits.Memory = &d.memory
		}
	}
	if cmd.Flags().Changed("pull-secret") {
		template.Spec.ImagePullSecrets = nil
		for _, name := range d.pullSecrets {
			template.Spec.ImagePullSecrets = append(template.Spec.ImagePullSecrets, platform.PullSecret{Name: name})
		}
	}
	return nil
}

func (a *app) appCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "app", Aliases: []string{"apps"}, Short: "Container apps: deploy them and read their output"}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List apps",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, _ []string) error {
			apps, err := client.ListApps(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(apps))
			for _, app := range apps {
				rows = append(rows, appRow(app))
			}
			return a.print(cmd, output.View{Data: apps, Columns: appColumns, Rows: rows, Empty: "No apps yet."})
		}),
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get <app>",
		Short: "Show an app",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			app, err := client.GetApp(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: app, Fields: appFields(app)})
		}),
	})

	var deploy appDeploy
	deployCmd := &cobra.Command{
		Use:   "deploy <app> --image <image>",
		Short: "Create an app, or roll out a new revision of it",
		Long: `Deploy a container image as an app. The app is created when it does not
exist; otherwise a new revision is rolled out, and settings that are not
given keep their value.

  rso app deploy api --image registry.example.com/acme/api:1.4.2 --port 8080 --env LOG_LEVEL=info

The app is served over HTTPS at the URL shown and scales with its traffic.`,
		Args: cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			name := args[0]
			existing, err := client.FindApp(cmd.Context(), name)
			if err != nil {
				return err
			}
			in := platform.AppInput{Name: name}
			if existing != nil {
				in.Template = existing.Template
				if len(existing.Metadata.Labels)+len(existing.Metadata.Annotations) > 0 {
					in.Metadata = &existing.Metadata
				}
			} else if deploy.image == "" {
				return usagef("--image is required to create app %s", name)
			}
			if err := deploy.apply(cmd, &in.Template); err != nil {
				return err
			}
			var app *platform.App
			if existing == nil {
				app, err = client.CreateApp(cmd.Context(), in)
			} else {
				app, err = client.UpdateApp(cmd.Context(), in)
			}
			if err != nil {
				return err
			}
			if deploy.wait {
				a.note(cmd, "Waiting for app %s to be ready...", name)
				if app, err = client.WaitAppReady(cmd.Context(), name, platform.WaitOptions{Timeout: deploy.timeout}); err != nil {
					return err
				}
			}
			return a.print(cmd, output.View{Data: app, Fields: appFields(app)})
		}),
	}
	flags := deployCmd.Flags()
	flags.StringVar(&deploy.image, "image", "", "container image to run")
	flags.IntVar(&deploy.port, "port", 0, "port the container listens on")
	flags.StringArrayVar(&deploy.env, "env", nil, "environment variable as NAME=value (repeatable)")
	flags.StringArrayVar(&deploy.secretEnv, "secret-env", nil, "environment variable from a secret as NAME=secret:key (repeatable)")
	flags.StringVar(&deploy.cpu, "cpu", "", "CPU limit, for example 500m or 2")
	flags.StringVar(&deploy.memory, "memory", "", "memory limit, for example 512Mi")
	flags.StringArrayVar(&deploy.pullSecrets, "pull-secret", nil, "secret with the credentials of a private registry (repeatable)")
	flags.BoolVar(&deploy.wait, "wait", false, "wait until the new revision serves traffic")
	flags.DurationVar(&deploy.timeout, "timeout", 5*time.Minute, "with --wait: how long to wait")
	cmd.AddCommand(deployCmd)

	var logs platform.LogOptions
	logsCmd := &cobra.Command{
		Use:   "logs <app>",
		Short: "Print the recent output of an app",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			result, err := client.AppLogs(cmd.Context(), args[0], logs)
			if err != nil {
				return err
			}
			return a.printLogs(cmd, result, result.Lines, result.Truncated, "The app has no output yet.")
		}),
	}
	logsCmd.Flags().StringVar(&logs.Pod, "pod", "", "only this instance of the app")
	logsCmd.Flags().IntVar(&logs.TailLines, "tail", 0, "how many lines from the end")
	logsCmd.Flags().IntVar(&logs.SinceSeconds, "since", 0, "only lines of the last N seconds")
	logsCmd.Flags().BoolVar(&logs.Previous, "previous", false, "output of the instance before the last restart")
	cmd.AddCommand(logsCmd)

	cmd.AddCommand(a.deleteCommand("delete <app>", "Delete an app",
		"Delete app %s? It stops serving at once.", "Deleted app %s.",
		a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			return client.DeleteApp(cmd.Context(), args[0])
		})))
	return cmd
}
