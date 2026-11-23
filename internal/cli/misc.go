package cli

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/theorigamicorporation/rso-cli/internal/output"
	"github.com/theorigamicorporation/rsodev-go/platform"
)

// quotaLabels names the resources the way people call them.
var quotaLabels = map[string]string{ //nolint:gosec // resource names, not credentials
	"vcpu": "vCPU", "ramGb": "Memory (GB)", "diskGb": "Disk (GB)", "vms": "Instances", "apps": "Apps",
	"pgClusters": "PostgreSQL clusters", "buckets": "Buckets", "k8sClusters": "Kubernetes clusters",
	"aiTokens": "AI tokens this month", "scheduledJobs": "Scheduled jobs", "queues": "Queues", "topics": "Topics",
	"webhooks": "Webhooks", "scheduledMessages": "Scheduled messages",
}

func (a *app) quotaCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "quota",
		Short: "Show what the tenant uses against its limits",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, _ []string) error {
			quota, err := client.Quota(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(quota))
			for _, q := range quota {
				percent := ""
				if q.Limit > 0 {
					percent = fmt.Sprintf("%.0f%%", q.Used/q.Limit*100)
				}
				rows = append(rows, []string{first(quotaLabels[q.Name], q.Name), count(q.Used), count(q.Limit), percent})
			}
			return a.print(cmd, output.View{Data: quota, Columns: []string{"RESOURCE", "USED", "LIMIT", "USE"}, Rows: rows})
		}),
	}
}

func (a *app) aiCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "ai", Short: "AI models: list them and ask one a question"}

	cmd.AddCommand(&cobra.Command{
		Use:   "models",
		Short: "List the models available for inference",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, _ []string) error {
			models, err := client.ListAIModels(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(models))
			for _, m := range models {
				rows = append(rows, []string{m.Name, m.Status, bytesize(m.Size), yesNo(m.Loaded), strings.Join(m.Capabilities, ",")})
			}
			return a.print(cmd, output.View{Data: models, Columns: []string{"NAME", "STATUS", "SIZE", "LOADED", "CAPABILITIES"}, Rows: rows, Empty: "No models yet."})
		}),
	})

	var model, system string
	chat := &cobra.Command{
		Use:   "chat --model <model> [prompt]",
		Short: "Ask a model a question and print the answer as it arrives",
		Long: `Ask a model a question. The prompt is the argument, or standard input
when it is left out or is "-".

The answer is billed to the tenant by token. A request may wait its turn on
the inference host before the answer starts.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if model == "" {
				return usagef(`--model is required: see "rso ai models"`)
			}
			prompt := ""
			if len(args) == 1 && args[0] != "-" {
				prompt = args[0]
			} else {
				var err error
				if prompt, err = a.readAll(); err != nil {
					return err
				}
			}
			if strings.TrimSpace(prompt) == "" {
				return usagef("no prompt given")
			}
			messages := []platform.ChatMessage{}
			if system != "" {
				messages = append(messages, platform.ChatMessage{Role: "system", Content: system})
			}
			messages = append(messages, platform.ChatMessage{Role: "user", Content: prompt})

			format, err := output.ParseFormat(a.format)
			if err != nil {
				return usageError{err}
			}
			client, err := a.client(false)
			if err != nil {
				return err
			}
			var answer strings.Builder
			usage, err := client.Chat(cmd.Context(), model, messages, func(text string) {
				answer.WriteString(text)
				if format == output.Table {
					_, _ = fmt.Fprint(cmd.OutOrStdout(), text)
				}
			})
			if err != nil {
				return err
			}
			if format != output.Table {
				return output.Write(cmd.OutOrStdout(), format, output.View{Data: map[string]any{"model": model, "answer": answer.String(), "usage": usage}})
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout())
			if usage != nil {
				a.note(cmd, "Tokens: %d in, %d out.", usage.PromptTokens, usage.CompletionTokens)
			}
			return nil
		},
	}
	chat.Flags().StringVar(&model, "model", "", "model to ask")
	chat.Flags().StringVar(&system, "system", "", "instructions for the model")
	cmd.AddCommand(chat)
	return cmd
}

func (a *app) versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of rso",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.print(cmd, output.View{
				Data:   map[string]string{"version": a.env.Version, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH},
				Fields: [][2]string{{"rso", a.env.Version}, {"Go", runtime.Version()}, {"Platform", runtime.GOOS + "/" + runtime.GOARCH}},
			})
		},
	}
}
