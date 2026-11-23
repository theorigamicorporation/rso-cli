package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/theorigamicorporation/rso-cli/internal/output"
	"github.com/theorigamicorporation/rsodev-go/platform"
)

var jobColumns = []string{"NAME", "SCHEDULE", "IMAGE", "SUSPENDED", "NEXT RUN", "LAST RUN"}

func jobRow(j platform.ScheduledJob) []string {
	last := ""
	if j.LastRun != nil {
		last = j.LastRun.Status
	}
	return []string{j.Name, j.Schedule, j.Image, yesNo(j.Suspended), when(j.NextRunAt), last}
}

func jobView(j *platform.ScheduledJob) output.View {
	last := ""
	if j.LastRun != nil {
		last = j.LastRun.Status + " (" + j.LastRun.Name + ")"
	}
	env := make([]string, 0, len(j.Env))
	for _, e := range j.Env {
		if e.SecretName != "" {
			env = append(env, e.Name+" (secret "+e.SecretName+"/"+e.SecretKey+")")
		} else {
			env = append(env, e.Name)
		}
	}
	return output.View{Data: j, Fields: [][2]string{
		{"Name", j.Name}, {"Image", j.Image},
		{"Command", strings.Join(j.Command, " ")}, {"Args", strings.Join(j.Args, " ")},
		{"Environment", strings.Join(env, ", ")},
		{"CPU", strconv.FormatFloat(j.CPU, 'f', -1, 64)}, {"Memory", strconv.Itoa(j.MemoryMB) + " MB"},
		{"Schedule (UTC)", j.Schedule}, {"Concurrency", j.Concurrency},
		{"Timeout", strconv.Itoa(j.TimeoutSeconds) + " s"}, {"Retries", strconv.Itoa(j.Retries)},
		{"Suspended", yesNo(j.Suspended)}, {"Next run", when(j.NextRunAt)},
		{"Last run", last},
	}}
}

var runColumns = []string{"RUN", "STATUS", "STARTED", "DURATION", "MANUAL", "MESSAGE"}

func runRow(r platform.JobRun) []string {
	duration := ""
	if r.FinishedAt != "" {
		duration = strconv.Itoa(r.DurationSeconds) + "s"
	}
	return []string{r.Name, r.Status, when(r.StartedAt), duration, yesNo(r.Manual), r.Message}
}

// jobFlags are the flags create and update share.
type jobFlags struct {
	image, cron, concurrency string
	cpu                      float64
	memory, timeout, retries int
	command, args, env       []string
	secretEnv, pullSecrets   []string
	suspended                bool
}

func (f *jobFlags) register(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVar(&f.image, "image", "", "container image to run")
	flags.StringVar(&f.cron, "cron", "", `when to run, a five-field cron expression in UTC, e.g. "*/30 * * * *"`)
	flags.Float64Var(&f.cpu, "cpu", 0.25, "CPU for the run, in cores")
	flags.IntVar(&f.memory, "memory", 256, "memory for the run, in MB")
	flags.StringArrayVar(&f.command, "command", nil, "override the image's entrypoint (repeat per word)")
	flags.StringArrayVar(&f.args, "arg", nil, "argument for the container (repeatable)")
	flags.StringArrayVar(&f.env, "env", nil, "environment variable, NAME=value (repeatable)")
	flags.StringArrayVar(&f.secretEnv, "secret-env", nil, "environment variable from a secret, NAME=secret/key (repeatable)")
	flags.StringArrayVar(&f.pullSecrets, "pull-secret", nil, "image pull secret (repeatable)")
	flags.StringVar(&f.concurrency, "concurrency", "", "what to do when a run is due while one is still going: forbid, allow or replace")
	flags.IntVar(&f.timeout, "timeout", 0, "seconds a run may take, retries included (default: the platform's)")
	flags.IntVar(&f.retries, "retries", 0, "how often a failed run is tried again (default: the platform's)")
	flags.BoolVar(&f.suspended, "suspended", false, "keep the schedule switched off")
}

func (f *jobFlags) environment() ([]platform.JobEnv, error) {
	var out []platform.JobEnv
	for _, pair := range f.env {
		name, value, found := strings.Cut(pair, "=")
		if !found || name == "" {
			return nil, usagef("--env takes NAME=value, got %q", pair)
		}
		out = append(out, platform.JobEnv{Name: name, Value: value})
	}
	for _, pair := range f.secretEnv {
		name, ref, found := strings.Cut(pair, "=")
		secret, key, ok := strings.Cut(ref, "/")
		if !found || !ok || name == "" || secret == "" || key == "" {
			return nil, usagef("--secret-env takes NAME=secret/key, got %q", pair)
		}
		out = append(out, platform.JobEnv{Name: name, SecretName: secret, SecretKey: key})
	}
	return out, nil
}

// apply puts the flags that were given on top of a definition.
func (f *jobFlags) apply(cmd *cobra.Command, input *platform.ScheduledJobInput, creating bool) error {
	changed := func(name string) bool { return creating || cmd.Flags().Changed(name) }
	if changed("image") {
		input.Image = f.image
	}
	if changed("cron") {
		input.Schedule = f.cron
	}
	if changed("cpu") {
		input.CPU = f.cpu
	}
	if changed("memory") {
		input.MemoryMB = f.memory
	}
	if cmd.Flags().Changed("command") {
		input.Command = f.command
	}
	if cmd.Flags().Changed("arg") {
		input.Args = f.args
	}
	if cmd.Flags().Changed("env") || cmd.Flags().Changed("secret-env") {
		env, err := f.environment()
		if err != nil {
			return err
		}
		input.Env = env
	}
	if cmd.Flags().Changed("pull-secret") {
		input.ImagePullSecrets = f.pullSecrets
	}
	if cmd.Flags().Changed("concurrency") {
		input.Concurrency = strings.ToUpper(f.concurrency)
	}
	if cmd.Flags().Changed("timeout") {
		input.TimeoutSeconds = &f.timeout
	}
	if cmd.Flags().Changed("retries") {
		input.Retries = &f.retries
	}
	if cmd.Flags().Changed("suspended") {
		input.Suspended = f.suspended
	}
	return nil
}

func (a *app) jobCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "job", Aliases: []string{"jobs"}, Short: "Run containers on a cron schedule"}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List scheduled jobs",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, api *platform.Client, _ []string) error {
			jobs, err := api.ListScheduledJobs(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(jobs))
			for _, j := range jobs {
				rows = append(rows, jobRow(j))
			}
			return a.print(cmd, output.View{Data: jobs, Columns: jobColumns, Rows: rows, Empty: "No scheduled jobs yet."})
		}),
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get <job>",
		Short: "Show a scheduled job",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, api *platform.Client, args []string) error {
			j, err := api.GetScheduledJob(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.print(cmd, jobView(j))
		}),
	})

	var createFlags jobFlags
	create := &cobra.Command{
		Use:   `create <job> --image <image> --cron "<expression>"`,
		Short: "Create a scheduled job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if createFlags.image == "" || createFlags.cron == "" {
				return usagef("--image and --cron are required")
			}
			input := platform.ScheduledJobInput{Name: args[0]}
			if err := createFlags.apply(cmd, &input, true); err != nil {
				return err
			}
			api, err := a.client(false)
			if err != nil {
				return err
			}
			j, err := api.CreateScheduledJob(cmd.Context(), input)
			if err != nil {
				return err
			}
			return a.print(cmd, jobView(j))
		},
	}
	createFlags.register(create)
	cmd.AddCommand(create)

	var updateFlags jobFlags
	update := &cobra.Command{
		Use:   "update <job>",
		Short: "Change a scheduled job; what is not given stays as it is",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, api *platform.Client, args []string) error {
			current, err := api.GetScheduledJob(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			input := platform.ScheduledJobInput{
				Name: current.Name, Image: current.Image, Command: current.Command, Args: current.Args,
				Env: current.Env, ImagePullSecrets: current.ImagePullSecrets, CPU: current.CPU, MemoryMB: current.MemoryMB,
				Schedule: current.Schedule, Concurrency: current.Concurrency, TimeoutSeconds: &current.TimeoutSeconds,
				Retries: &current.Retries, Suspended: current.Suspended,
			}
			if err := updateFlags.apply(cmd, &input, false); err != nil {
				return err
			}
			j, err := api.UpdateScheduledJob(cmd.Context(), input)
			if err != nil {
				return err
			}
			return a.print(cmd, jobView(j))
		}),
	}
	updateFlags.register(update)
	cmd.AddCommand(update)

	suspend := func(use, short string, suspended bool) *cobra.Command {
		return &cobra.Command{
			Use:   use,
			Short: short,
			Args:  cobra.ExactArgs(1),
			RunE: a.sdk(func(cmd *cobra.Command, api *platform.Client, args []string) error {
				if err := api.SetScheduledJobSuspended(cmd.Context(), args[0], suspended); err != nil {
					return err
				}
				j, err := api.GetScheduledJob(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return a.print(cmd, jobView(j))
			}),
		}
	}
	cmd.AddCommand(suspend("suspend <job>", "Stop the schedule without deleting the job", true))
	cmd.AddCommand(suspend("resume <job>", "Start the schedule again", false))

	cmd.AddCommand(&cobra.Command{
		Use:   "run <job>",
		Short: "Start a run now, outside the schedule",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, api *platform.Client, args []string) error {
			r, err := api.RunScheduledJob(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: r, Columns: runColumns, Rows: [][]string{runRow(*r)}})
		}),
	})

	var limit int
	runs := &cobra.Command{
		Use:   "runs <job>",
		Short: "List the recent runs of a job",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, api *platform.Client, args []string) error {
			list, err := api.ListScheduledJobRuns(cmd.Context(), args[0], limit)
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(list))
			for _, r := range list {
				rows = append(rows, runRow(r))
			}
			return a.print(cmd, output.View{Data: list, Columns: runColumns, Rows: rows, Empty: "The job has not run yet."})
		}),
	}
	runs.Flags().IntVar(&limit, "limit", 0, "how many runs to show (default: the platform's)")
	cmd.AddCommand(runs)

	var logs platform.LogOptions
	var run string
	logsCmd := &cobra.Command{
		Use:   "logs <job>",
		Short: "Print the output of a run (the last one by default)",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			result, err := client.ScheduledJobLogs(cmd.Context(), args[0], run, logs)
			if err != nil {
				return err
			}
			return a.printLogs(cmd, result, result.Lines, result.Truncated, "The job has no output yet.")
		}),
	}
	logsCmd.Flags().StringVar(&run, "run", "", "the run to read (default: the most recent)")
	logsCmd.Flags().IntVar(&logs.TailLines, "tail", 0, "how many lines from the end")
	logsCmd.Flags().IntVar(&logs.SinceSeconds, "since", 0, "only lines of the last N seconds")
	cmd.AddCommand(logsCmd)

	cmd.AddCommand(a.deleteCommand("delete <job>", "Delete a scheduled job and its run history",
		"Delete scheduled job %s and its run history?", "Deleted scheduled job %s.",
		a.sdk(func(cmd *cobra.Command, api *platform.Client, args []string) error {
			return api.DeleteScheduledJob(cmd.Context(), args[0])
		})))
	return cmd
}

// printLogs writes log lines as plain text for people, or the whole answer
// for programs.
func (a *app) printLogs(cmd *cobra.Command, data any, lines []platform.LogLine, truncated bool, empty string) error {
	format, err := output.ParseFormat(a.format)
	if err != nil {
		return usageError{err}
	}
	if format != output.Table {
		return output.Write(cmd.OutOrStdout(), format, output.View{Data: data})
	}
	if len(lines) == 0 {
		a.note(cmd, "%s", empty)
		return nil
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), line.Message); err != nil {
			return err
		}
	}
	if truncated {
		a.note(cmd, "Older lines were left out; use --tail or --since to choose.")
	}
	return nil
}
