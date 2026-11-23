package cli

import (
	"strconv"

	"github.com/spf13/cobra"

	"github.com/theorigamicorporation/rso-cli/internal/output"
	"github.com/theorigamicorporation/rsodev-go/platform"
)

var subscriptionColumns = []string{"ID", "TOPIC", "FILTER", "TARGET", "STATUS"}

func subscriptionRow(s platform.Subscription) []string {
	target := "queue " + s.Queue
	if s.Webhook != "" {
		target = "webhook " + s.Webhook
	}
	return []string{s.ID, s.Topic, s.Filter, target, string(s.Status)}
}

// subscriptionData is a subscription with its target spelled out; the SDK
// type keeps the target out of its JSON form.
func subscriptionData(s platform.Subscription) map[string]any {
	target := map[string]string{"queue": s.Queue}
	if s.Webhook != "" {
		target = map[string]string{"webhook": s.Webhook}
	}
	return map[string]any{"id": s.ID, "topic": s.Topic, "filter": s.Filter, "target": target, "status": s.Status, "createdAt": s.CreatedAt}
}

func (a *app) topicCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "topic", Aliases: []string{"topics"}, Short: "Manage topics and the subscriptions that receive their messages"}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List topics",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, api *platform.Client, _ []string) error {
			topics, err := api.ListTopics(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(topics))
			for _, t := range topics {
				rows = append(rows, []string{t.Name, string(t.Status), strconv.Itoa(t.SubscriptionCount), when(t.CreatedAt)})
			}
			return a.print(cmd, output.View{Data: topics, Columns: []string{"NAME", "STATUS", "SUBSCRIPTIONS", "CREATED"}, Rows: rows, Empty: "No topics yet."})
		}),
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "create <topic>",
		Short: "Create a topic",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, api *platform.Client, args []string) error {
			t, err := api.CreateTopic(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: t, Columns: []string{"NAME", "STATUS", "SUBSCRIPTIONS", "CREATED"},
				Rows: [][]string{{t.Name, string(t.Status), strconv.Itoa(t.SubscriptionCount), when(t.CreatedAt)}}})
		}),
	})

	cmd.AddCommand(a.deleteCommand("delete <topic>", "Delete a topic and its subscriptions",
		"Delete topic %s and its subscriptions?", "Deleted topic %s.",
		a.sdk(func(cmd *cobra.Command, api *platform.Client, args []string) error {
			return api.DeleteTopic(cmd.Context(), args[0])
		})))

	cmd.AddCommand(a.publishCommand("publish <topic> [message]", "Publish a message to a topic", true))

	var filter string
	subscribe := &cobra.Command{
		Use:   "subscribe <topic> <queue>",
		Short: "Deliver a topic's messages into a queue",
		Long: `Deliver a topic's messages into a queue.

--filter is a pattern on the routing key a message was published with:
words separated by dots, * for exactly one word, # for any number of words.
Without a filter the queue receives every message of the topic.`,
		Args: cobra.ExactArgs(2),
		RunE: a.sdk(func(cmd *cobra.Command, api *platform.Client, args []string) error {
			s, err := api.SubscribeQueue(cmd.Context(), args[0], args[1], filter)
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: subscriptionData(*s), Columns: subscriptionColumns, Rows: [][]string{subscriptionRow(*s)}})
		}),
	}
	subscribe.Flags().StringVar(&filter, "filter", "", "routing key pattern, e.g. order.* (default: every message)")
	cmd.AddCommand(subscribe)

	var ofTopic, intoQueue string
	subscriptions := &cobra.Command{
		Use:   "subscriptions",
		Short: "List subscriptions",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, api *platform.Client, _ []string) error {
			subscriptions, err := api.ListSubscriptions(cmd.Context(), ofTopic)
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(subscriptions))
			data := make([]map[string]any, 0, len(subscriptions))
			for _, s := range subscriptions {
				if intoQueue != "" && s.Queue != intoQueue {
					continue
				}
				rows = append(rows, subscriptionRow(s))
				data = append(data, subscriptionData(s))
			}
			return a.print(cmd, output.View{Data: data, Columns: subscriptionColumns, Rows: rows, Empty: "No subscriptions yet."})
		}),
	}
	subscriptions.Flags().StringVar(&ofTopic, "topic", "", "only subscriptions of this topic")
	subscriptions.Flags().StringVar(&intoQueue, "queue", "", "only subscriptions into this queue")
	cmd.AddCommand(subscriptions)

	cmd.AddCommand(a.deleteCommand("unsubscribe <subscription-id>", "Remove a subscription",
		"Remove subscription %s?", "Removed subscription %s.",
		a.sdk(func(cmd *cobra.Command, api *platform.Client, args []string) error {
			return api.DeleteSubscription(cmd.Context(), args[0])
		})))
	return cmd
}

func (a *app) eventsCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "events", Short: "Platform events: what happens in the tenant, as messages"}
	cmd.AddCommand(&cobra.Command{
		Use:   "types",
		Short: "List the event types and the topic they are published to",
		Long: `List the platform event types.

Events are published to the topic shown; subscribe a queue to it with
"rso topic subscribe <topic> <queue> --filter 'apps.*'", or a webhook with
"rso webhook create --topic <topic>". Delivery of events is best effort.`,
		Args: cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, _ []string) error {
			events, err := client.PlatformEvents(cmd.Context())
			if err != nil {
				return err
			}
			if events.Available {
				a.note(cmd, "Events are published to topic %s.", events.Topic)
			} else {
				a.note(cmd, "Platform events are not switched on here.")
			}
			rows := make([][]string, 0, len(events.Types))
			for _, t := range events.Types {
				rows = append(rows, []string{t.Type, t.Description})
			}
			return a.print(cmd, output.View{Data: events, Columns: []string{"TYPE", "DESCRIPTION"}, Rows: rows, Empty: "No event types."})
		}),
	})
	return cmd
}
