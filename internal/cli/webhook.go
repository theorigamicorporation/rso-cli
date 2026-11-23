package cli

import (
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/theorigamicorporation/rso-cli/internal/output"
	"github.com/theorigamicorporation/rsodev-go/platform"
)

var webhookColumns = []string{"NAME", "SOURCE", "URL", "STATE", "DELIVERED", "DEAD", "LAST"}

func webhookSource(w platform.Webhook) string {
	if w.Queue != "" {
		return "queue " + w.Queue
	}
	if w.Filter != "" && w.Filter != "#" {
		return "topic " + w.Topic + " (" + w.Filter + ")"
	}
	return "topic " + w.Topic
}

func webhookRow(w platform.Webhook) []string {
	last := ""
	if w.LastStatus != 0 {
		last = strconv.Itoa(w.LastStatus)
	}
	return []string{w.Name, webhookSource(w), w.URL, w.State, count(w.DeliveredCount), count(w.DeadLetteredCount), last}
}

func webhookView(w *platform.Webhook) output.View {
	last := ""
	if w.LastStatus != 0 {
		last = strconv.Itoa(w.LastStatus)
	}
	return output.View{Data: w, Fields: [][2]string{
		{"Name", w.Name}, {"Source", webhookSource(*w)}, {"URL", w.URL},
		{"Signed", yesNo(w.HasSecret)}, {"Headers", strings.Join(w.HeaderNames, ", ")},
		{"State", w.State}, {"Pause reason", w.PauseReason},
		{"Delivered", count(w.DeliveredCount)}, {"Dead-lettered", count(w.DeadLetteredCount)},
		{"Dead-letter queue", w.DeadLetterQueue},
		{"Last status", last}, {"Last error", w.LastError},
		{"Last attempt", when(w.LastAttemptAt)}, {"Last success", when(w.LastSuccessAt)},
	}}
}

// headers parses repeated --header name=value flags.
func headers(pairs []string) (map[string]string, error) {
	return keyValues("header", pairs)
}

func (a *app) webhookCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "webhook", Aliases: []string{"webhooks"}, Short: "Deliver messages of a topic or queue to a URL"}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List webhooks",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, api *platform.Client, _ []string) error {
			webhooks, err := api.ListWebhooks(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(webhooks))
			for _, w := range webhooks {
				rows = append(rows, webhookRow(w))
			}
			return a.print(cmd, output.View{Data: webhooks, Columns: webhookColumns, Rows: rows, Empty: "No webhooks yet."})
		}),
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get <webhook>",
		Short: "Show a webhook and how its deliveries are going",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, api *platform.Client, args []string) error {
			w, err := api.GetWebhook(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.print(cmd, webhookView(w))
		}),
	})

	var topic, queue, filter, target string
	var secretStdin bool
	var headerPairs []string
	create := &cobra.Command{
		Use:   "create <webhook> --url <https url> (--topic <topic> | --queue <queue>)",
		Short: "Create a webhook",
		Long: `Create a webhook that delivers every message of a topic (optionally
filtered) or of a queue to an https URL.

Delivery is at least once: a message leaves only after the URL answered 2xx,
failures are retried, and what cannot be delivered is parked in the
webhook's dead-letter queue. The same message can arrive twice; duplicates
carry the same delivery id header.

With --secret-stdin the signing secret is read from standard input, and each
delivery is signed with it so the receiver can check where it came from.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := exactlyOne("topic", topic, "queue", queue); err != nil {
				return err
			}
			if target == "" {
				return usagef("--url is required")
			}
			if filter != "" && topic == "" {
				return usagef("--filter only applies with --topic")
			}
			list, err := headers(headerPairs)
			if err != nil {
				return err
			}
			input := platform.WebhookInput{Name: args[0], Topic: topic, Filter: filter, Queue: queue, URL: target, Headers: list}
			if secretStdin {
				raw, err := a.readAll()
				if err != nil {
					return err
				}
				if input.Secret = strings.TrimSpace(raw); input.Secret == "" {
					return usagef("no secret on standard input")
				}
			}
			api, err := a.client(false)
			if err != nil {
				return err
			}
			w, err := api.CreateWebhook(cmd.Context(), input)
			if err != nil {
				return err
			}
			return a.print(cmd, webhookView(w))
		},
	}
	create.Flags().StringVar(&target, "url", "", "https URL to deliver to")
	create.Flags().StringVar(&topic, "topic", "", "deliver the messages of this topic")
	create.Flags().StringVar(&queue, "queue", "", "deliver the messages of this queue")
	create.Flags().StringVar(&filter, "filter", "", "with --topic: routing key pattern, e.g. order.*")
	create.Flags().BoolVar(&secretStdin, "secret-stdin", false, "read the signing secret from standard input")
	create.Flags().StringArrayVar(&headerPairs, "header", nil, "static header to send, name=value (repeatable)")
	cmd.AddCommand(create)

	var newURL string
	var newSecretStdin, removeSecret bool
	var newHeaders []string
	update := &cobra.Command{
		Use:   "update <webhook>",
		Short: "Change a webhook's URL, secret or headers",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var input platform.WebhookUpdate
			if cmd.Flags().Changed("url") {
				input.URL = &newURL
			}
			if cmd.Flags().Changed("header") {
				list, err := headers(newHeaders)
				if err != nil {
					return err
				}
				input.Headers, input.ReplaceHeaders = list, true
			}
			switch {
			case newSecretStdin && removeSecret:
				return usagef("give at most one of --secret-stdin and --remove-secret")
			case removeSecret:
				empty := ""
				input.Secret = &empty
			case newSecretStdin:
				raw, err := a.readAll()
				if err != nil {
					return err
				}
				secret := strings.TrimSpace(raw)
				if secret == "" {
					return usagef("no secret on standard input")
				}
				input.Secret = &secret
			}
			if input.URL == nil && !input.ReplaceHeaders && input.Secret == nil {
				return usagef("nothing to change: give --url, --header, --secret-stdin or --remove-secret")
			}
			api, err := a.client(false)
			if err != nil {
				return err
			}
			w, err := api.UpdateWebhook(cmd.Context(), args[0], input)
			if err != nil {
				return err
			}
			return a.print(cmd, webhookView(w))
		},
	}
	update.Flags().StringVar(&newURL, "url", "", "new https URL")
	update.Flags().BoolVar(&newSecretStdin, "secret-stdin", false, "read a new signing secret from standard input")
	update.Flags().BoolVar(&removeSecret, "remove-secret", false, "stop signing deliveries")
	update.Flags().StringArrayVar(&newHeaders, "header", nil, "replace the static headers, name=value (repeatable)")
	cmd.AddCommand(update)

	setEnabled := func(use, short string, enabled bool) *cobra.Command {
		return &cobra.Command{
			Use:   use,
			Short: short,
			Args:  cobra.ExactArgs(1),
			RunE: a.sdk(func(cmd *cobra.Command, api *platform.Client, args []string) error {
				w, err := api.UpdateWebhook(cmd.Context(), args[0], platform.WebhookUpdate{Enabled: &enabled})
				if err != nil {
					return err
				}
				return a.print(cmd, webhookView(w))
			}),
		}
	}
	cmd.AddCommand(setEnabled("disable <webhook>", "Stop delivering; messages wait until it is enabled again", false))
	cmd.AddCommand(setEnabled("enable <webhook>", "Deliver again after a disable", true))

	cmd.AddCommand(&cobra.Command{
		Use:   "resume <webhook>",
		Short: "Start a webhook again after it paused itself on repeated failures",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, api *platform.Client, args []string) error {
			if err := api.ResumeWebhook(cmd.Context(), args[0]); err != nil {
				return err
			}
			w, err := api.GetWebhook(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.print(cmd, webhookView(w))
		}),
	})

	cmd.AddCommand(a.deleteCommand("delete <webhook>", "Delete a webhook",
		"Delete webhook %s? Messages still waiting for it are dropped.", "Deleted webhook %s.",
		a.sdk(func(cmd *cobra.Command, api *platform.Client, args []string) error {
			return api.DeleteWebhook(cmd.Context(), args[0])
		})))
	return cmd
}
