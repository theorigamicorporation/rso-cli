package cli

import (
	"github.com/spf13/cobra"

	"github.com/theorigamicorporation/rso-cli/internal/output"
	"github.com/theorigamicorporation/rsodev-go/platform"
)

var scheduleColumns = []string{"NAME", "SCHEDULE", "TARGET", "ENABLED", "NEXT RUN", "LAST"}

func scheduleTarget(m platform.ScheduledMessage) string {
	if m.Queue != "" {
		return "queue " + m.Queue
	}
	if m.RoutingKey != "" {
		return "topic " + m.Topic + " (" + m.RoutingKey + ")"
	}
	return "topic " + m.Topic
}

func scheduleRow(m platform.ScheduledMessage) []string {
	return []string{m.Name, m.Schedule, scheduleTarget(m), yesNo(m.Enabled), when(m.NextRunAt), m.LastStatus}
}

func scheduleView(m *platform.ScheduledMessage) output.View {
	return output.View{Data: m, Fields: [][2]string{
		{"Name", m.Name}, {"Schedule (UTC)", m.Schedule}, {"Target", scheduleTarget(*m)},
		{"Content type", m.ContentType}, {"Body", m.Body}, {"Enabled", yesNo(m.Enabled)},
		{"Next run", when(m.NextRunAt)}, {"Last run", when(m.LastRunAt)},
		{"Last status", m.LastStatus}, {"Last error", m.LastError}, {"Runs", count(m.RunCount)},
	}}
}

func (a *app) scheduleCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "schedule", Aliases: []string{"schedules"}, Short: "Publish a message to a queue or topic on a cron schedule"}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List scheduled messages",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, api *platform.Client, _ []string) error {
			items, err := api.ListScheduledMessages(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(items))
			for _, m := range items {
				rows = append(rows, scheduleRow(m))
			}
			return a.print(cmd, output.View{Data: items, Columns: scheduleColumns, Rows: rows, Empty: "No scheduled messages yet."})
		}),
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get <name>",
		Short: "Show a scheduled message",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, api *platform.Client, args []string) error {
			m, err := api.GetScheduledMessage(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.print(cmd, scheduleView(m))
		}),
	})

	// create and update take the whole definition, as the API does.
	define := func(use, short string, update bool) *cobra.Command {
		var cron, queue, topic, routingKey, body, contentType string
		var disabled bool
		c := &cobra.Command{
			Use:   use,
			Short: short,
			Long: short + `.

--cron is a five-field cron expression in UTC (minute hour day month
weekday). The message is --body, or standard input when --body is "-". A
message is published at least once per occurrence.`,
			Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				if cron == "" {
					return usagef("--cron is required")
				}
				if err := exactlyOne("queue", queue, "topic", topic); err != nil {
					return err
				}
				if routingKey != "" && topic == "" {
					return usagef("--routing-key only applies with --topic")
				}
				if body == "-" {
					var err error
					if body, err = a.readAll(); err != nil {
						return err
					}
				}
				input := platform.ScheduledMessageInput{Name: args[0], Schedule: cron, Queue: queue, Topic: topic, RoutingKey: routingKey, Body: body, ContentType: contentType, Enabled: !disabled}
				api, err := a.client(false)
				if err != nil {
					return err
				}
				var m *platform.ScheduledMessage
				if update {
					m, err = api.UpdateScheduledMessage(cmd.Context(), input)
				} else {
					m, err = api.CreateScheduledMessage(cmd.Context(), input)
				}
				if err != nil {
					return err
				}
				return a.print(cmd, scheduleView(m))
			},
		}
		c.Flags().StringVar(&cron, "cron", "", `when to publish, e.g. "0 6 * * 1-5"`)
		c.Flags().StringVar(&queue, "queue", "", "publish into this queue")
		c.Flags().StringVar(&topic, "topic", "", "publish to this topic")
		c.Flags().StringVar(&routingKey, "routing-key", "", "with --topic: routing key of the message")
		c.Flags().StringVar(&body, "body", "", `the message; "-" reads standard input`)
		c.Flags().StringVar(&contentType, "content-type", "", "content type of the message")
		c.Flags().BoolVar(&disabled, "disabled", false, "keep the schedule switched off")
		return c
	}
	cmd.AddCommand(define("create <name>", "Create a scheduled message", false))
	cmd.AddCommand(define("update <name>", "Replace a scheduled message's definition", true))

	cmd.AddCommand(&cobra.Command{
		Use:   "run <name>",
		Short: "Publish the message now, outside its schedule",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, api *platform.Client, args []string) error {
			if err := api.RunScheduledMessage(cmd.Context(), args[0]); err != nil {
				return err
			}
			m, err := api.GetScheduledMessage(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.print(cmd, scheduleView(m))
		}),
	})

	cmd.AddCommand(a.deleteCommand("delete <name>", "Delete a scheduled message",
		"Delete scheduled message %s?", "Deleted scheduled message %s.",
		a.sdk(func(cmd *cobra.Command, api *platform.Client, args []string) error {
			return api.DeleteScheduledMessage(cmd.Context(), args[0])
		})))
	return cmd
}
