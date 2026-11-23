package cli

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/theorigamicorporation/rso-cli/internal/output"
	"github.com/theorigamicorporation/rsodev-go/platform"
)

func queueRow(q platform.Queue) []string {
	return []string{q.Name, string(q.Status), count(q.MessagesReady), count(q.MessagesUnacknowledged), strconv.Itoa(q.Consumers), when(q.CreatedAt)}
}

var queueColumns = []string{"NAME", "STATUS", "WAITING", "UNACKED", "CONSUMERS", "CREATED"}

func (a *app) queueCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "queue", Aliases: []string{"queues"}, Short: "Manage queues and move messages through them"}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List queues",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			api, err := a.client(false)
			if err != nil {
				return err
			}
			queues, err := api.ListQueues(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(queues))
			for _, q := range queues {
				rows = append(rows, queueRow(q))
			}
			return a.print(cmd, output.View{Data: queues, Columns: queueColumns, Rows: rows, Empty: "No queues yet."})
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get <queue>",
		Short: "Show one queue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			api, err := a.client(false)
			if err != nil {
				return err
			}
			q, err := api.GetQueue(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: q, Fields: [][2]string{
				{"Name", q.Name}, {"Status", string(q.Status)}, {"Reason", q.StatusReason},
				{"Waiting", count(q.MessagesReady)}, {"Unacknowledged", count(q.MessagesUnacknowledged)},
				{"Consumers", strconv.Itoa(q.Consumers)}, {"Created", when(q.CreatedAt)},
			}})
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "create <queue>",
		Short: "Create a queue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			api, err := a.client(false)
			if err != nil {
				return err
			}
			q, err := api.CreateQueue(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: q, Columns: queueColumns, Rows: [][]string{queueRow(*q)}})
		},
	})

	var yes bool
	del := &cobra.Command{
		Use:   "delete <queue>",
		Short: "Delete a queue and the messages in it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.confirm(cmd, yes, fmt.Sprintf("Delete queue %s and every message in it?", args[0])); err != nil {
				return err
			}
			api, err := a.client(false)
			if err != nil {
				return err
			}
			if err := api.DeleteQueue(cmd.Context(), args[0]); err != nil {
				return err
			}
			a.note(cmd, "Deleted queue %s.", args[0])
			return nil
		},
	}
	del.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	cmd.AddCommand(del)

	var purgeYes bool
	purge := &cobra.Command{
		Use:   "purge <queue>",
		Short: "Remove every message waiting in a queue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.confirm(cmd, purgeYes, fmt.Sprintf("Remove every message waiting in queue %s?", args[0])); err != nil {
				return err
			}
			api, err := a.client(false)
			if err != nil {
				return err
			}
			if err := api.PurgeQueue(cmd.Context(), args[0]); err != nil {
				return err
			}
			a.note(cmd, "Purged queue %s.", args[0])
			return nil
		},
	}
	purge.Flags().BoolVarP(&purgeYes, "yes", "y", false, "do not ask for confirmation")
	cmd.AddCommand(purge)

	cmd.AddCommand(a.publishCommand("publish <queue> [message]", "Put a message into a queue", false))

	var limit int
	receive := &cobra.Command{
		Use:   "receive <queue>",
		Short: "Take messages from a queue",
		Long: `Take up to --max messages from a queue and print them.

The messages are removed from the queue as they are handed out: if this
command or what reads its output fails afterwards, they are gone. Where a
message must survive a crashed consumer, consume over AMQP or deliver with a
webhook ("rso webhook create").`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			api, err := a.client(false)
			if err != nil {
				return err
			}
			messages, err := api.Receive(cmd.Context(), args[0], limit)
			if err != nil {
				return err
			}
			if messages == nil {
				messages = []platform.ReceivedMessage{}
			}
			rows := make([][]string, 0, len(messages))
			for _, m := range messages {
				rows = append(rows, []string{when(m.PublishedAt), m.Topic, m.RoutingKey, m.Body})
			}
			return a.print(cmd, output.View{Data: messages, Columns: []string{"PUBLISHED", "TOPIC", "ROUTING KEY", "BODY"}, Rows: rows, Empty: "The queue is empty."})
		},
	}
	receive.Flags().IntVar(&limit, "max", 1, "how many messages to take at most")
	cmd.AddCommand(receive)

	cmd.AddCommand(&cobra.Command{
		Use:   "info",
		Short: "Show how to reach the queues: endpoints and limits",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			api, err := a.client(false)
			if err != nil {
				return err
			}
			info, err := api.QueuesInfo(cmd.Context())
			if err != nil {
				return err
			}
			amqp := ""
			if info.AMQPHost != "" {
				amqp = fmt.Sprintf("%s:%d", info.AMQPHost, info.AMQPPort)
			}
			return a.print(cmd, output.View{Data: info, Fields: [][2]string{
				{"Available", yesNo(info.Available)}, {"HTTP endpoint", info.HTTPEndpoint},
				{"AMQP (in-cluster)", amqp}, {"Virtual host", info.Vhost},
				{"Largest message", bytesize(float64(info.MaxMessageBytes))},
				{"Queue holds at most", fmt.Sprintf("%d messages or %s", info.MaxQueueLength, bytesize(info.MaxQueueBytes))},
				{"Messages kept for", fmt.Sprintf("%d h", info.MessageRetentionSeconds/3600)},
				{"Receive at most", strconv.Itoa(info.MaxReceive)},
			}})
		},
	})

	cmd.AddCommand(a.queueCredentialCommand())
	return cmd
}

// publishCommand is shared by "queue publish" and "topic publish".
func (a *app) publishCommand(use, short string, toTopic bool) *cobra.Command {
	var contentType, routingKey, messageID string
	var attributes []string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Long:  short + ".\n\nThe message is the second argument, or standard input when it is left out or is \"-\".",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := ""
			if len(args) == 2 && args[1] != "-" {
				body = args[1]
			} else {
				var err error
				if body, err = a.readAll(); err != nil {
					return err
				}
			}
			attrs, err := keyValues("attribute", attributes)
			if err != nil {
				return err
			}
			message := platform.Message{Body: body, ContentType: contentType, Attributes: attrs, MessageID: messageID}
			if asJSON {
				if !json.Valid([]byte(body)) {
					return usagef("--json was given but the message is not valid JSON")
				}
				message.Body, message.JSON = "", json.RawMessage(body)
			}
			api, err := a.client(false)
			if err != nil {
				return err
			}
			var routed bool
			if toTopic {
				routed, err = api.PublishToTopic(cmd.Context(), args[0], routingKey, message)
			} else {
				routed, err = api.Publish(cmd.Context(), args[0], message)
			}
			if err != nil {
				return err
			}
			if !routed {
				a.note(cmd, "Published, but no subscription matched: the message was dropped.")
			}
			return a.print(cmd, output.View{Data: map[string]bool{"routed": routed}, Fields: [][2]string{{"Routed", yesNo(routed)}}})
		},
	}
	cmd.Flags().StringVar(&contentType, "content-type", "", "content type of the message")
	cmd.Flags().StringArrayVar(&attributes, "attribute", nil, "attribute to send with the message, name=value (repeatable)")
	cmd.Flags().StringVar(&messageID, "message-id", "", "id for the message, to recognise a duplicate")
	cmd.Flags().BoolVar(&asJSON, "json", false, "send the message as a JSON document")
	if toTopic {
		cmd.Flags().StringVar(&routingKey, "routing-key", "", "routing key subscriptions filter on, e.g. order.created")
	}
	return cmd
}

func (a *app) queueCredentialCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "credential", Aliases: []string{"credentials"}, Short: "Manage AMQP credentials for apps inside the cluster"}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List AMQP credentials",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			api, err := a.client(false)
			if err != nil {
				return err
			}
			credentials, err := api.ListQueueCredentials(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(credentials))
			// A listing has no secrets; the SDK's type has fields for them, so
			// only what a listing knows is printed.
			data := make([]map[string]any, 0, len(credentials))
			for _, c := range credentials {
				rows = append(rows, []string{c.Name, string(c.Status), when(c.CreatedAt)})
				data = append(data, map[string]any{"name": c.Name, "status": c.Status, "createdAt": c.CreatedAt})
			}
			return a.print(cmd, output.View{Data: data, Columns: []string{"NAME", "STATUS", "CREATED"}, Rows: rows, Empty: "No credentials yet."})
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "create <name>",
		Short: "Issue an AMQP credential; its password is shown once",
		Long: `Issue an AMQP credential for the tenant's queues.

The password is printed once and cannot be read again. The credential works
from inside the cluster, and it may publish and consume but not declare
queues: clients that declare on start need passive declares.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			api, err := a.client(false)
			if err != nil {
				return err
			}
			c, err := api.CreateQueueCredential(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: c, Fields: [][2]string{
				{"Name", c.Name}, {"Username", c.Username}, {"Password", c.Password},
				{"Host", fmt.Sprintf("%s:%d", c.Host, c.Port)}, {"Virtual host", c.Vhost}, {"URI", c.URI},
			}})
		},
	})

	var yes bool
	del := &cobra.Command{
		Use:   "delete <name>",
		Short: "Revoke an AMQP credential",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.confirm(cmd, yes, fmt.Sprintf("Revoke credential %s? Clients using it stop working.", args[0])); err != nil {
				return err
			}
			api, err := a.client(false)
			if err != nil {
				return err
			}
			if err := api.DeleteQueueCredential(cmd.Context(), args[0]); err != nil {
				return err
			}
			a.note(cmd, "Revoked credential %s.", args[0])
			return nil
		},
	}
	del.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	cmd.AddCommand(del)
	return cmd
}
