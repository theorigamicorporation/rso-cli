package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/theorigamicorporation/rso-cli/internal/output"
	"github.com/theorigamicorporation/rsodev-go/platform"
)

var bucketColumns = []string{"NAME", "S3 BUCKET", "OBJECTS", "SIZE", "CREATED"}

func bucketRow(b platform.Bucket) []string {
	return []string{b.Name, b.Bucket, count(b.ObjectCount), bytesize(b.SizeBytes), when(deref(b.CreatedAt))}
}

var notificationColumns = []string{"ID", "BUCKET", "EVENTS", "KEYS", "TARGET", "DELIVERED", "DROPPED"}

func notificationRow(n platform.BucketNotification) []string {
	keys := n.KeyPrefix + "*" + n.KeySuffix
	target := "queue " + n.Queue
	if n.Topic != "" {
		target = "topic " + n.Topic
		if n.RoutingKey != "" {
			target += " (" + n.RoutingKey + ")"
		}
	}
	return []string{n.ID, n.Bucket, strings.ToLower(strings.Join(n.Events, ",")), keys, target, count(n.Delivered), count(n.Dropped)}
}

func (a *app) bucketCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "bucket", Aliases: []string{"buckets"}, Short: "Manage object storage buckets, access keys and notifications"}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List buckets",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, _ []string) error {
			buckets, err := client.ListBuckets(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(buckets))
			for _, b := range buckets {
				rows = append(rows, bucketRow(b))
			}
			return a.print(cmd, output.View{Data: buckets, Columns: bucketColumns, Rows: rows, Empty: "No buckets yet."})
		}),
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "create <bucket>",
		Short: "Create a bucket",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			b, err := client.CreateBucket(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: b, Fields: [][2]string{
				{"Name", b.Name}, {"S3 bucket", b.Bucket}, {"Endpoint", b.Endpoint}, {"Region", b.Region},
			}})
		}),
	})

	cmd.AddCommand(a.deleteCommand("delete <bucket>", "Delete a bucket",
		"Delete bucket %s?", "Deleted bucket %s.",
		a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			return client.DeleteBucket(cmd.Context(), args[0])
		})))

	cmd.AddCommand(a.bucketKeyCommand(), a.bucketNotificationCommand())
	return cmd
}

func (a *app) bucketKeyCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "key", Aliases: []string{"keys"}, Short: "Manage S3 access keys"}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List S3 access keys",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, _ []string) error {
			keys, err := client.ListStorageKeys(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(keys))
			for _, k := range keys {
				rows = append(rows, []string{k.Name, k.AccessKeyID, when(deref(k.CreatedAt))})
			}
			return a.print(cmd, output.View{Data: keys, Columns: []string{"NAME", "ACCESS KEY ID", "CREATED"}, Rows: rows, Empty: "No access keys yet."})
		}),
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "create <name>",
		Short: "Issue an S3 access key; its secret is shown once",
		Long: `Issue an S3 access key for the tenant's buckets.

The secret access key is printed once and cannot be read again. A key
reaches every bucket of the tenant.`,
		Args: cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			k, err := client.CreateStorageKey(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: k, Fields: [][2]string{
				{"Name", k.Name}, {"Access key id", k.AccessKeyID}, {"Secret access key", k.SecretAccessKey},
			}})
		}),
	})

	cmd.AddCommand(a.deleteCommand("delete <name>", "Revoke an S3 access key",
		"Revoke access key %s? Clients using it stop working.", "Revoked access key %s.",
		a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			return client.DeleteStorageKey(cmd.Context(), args[0])
		})))
	return cmd
}

func (a *app) bucketNotificationCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "notification", Aliases: []string{"notifications"}, Short: "Send a message when objects in a bucket are created or removed"}

	var ofBucket string
	list := &cobra.Command{
		Use:   "list",
		Short: "List bucket notifications",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, api *platform.Client, _ []string) error {
			items, err := api.ListBucketNotifications(cmd.Context(), ofBucket)
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(items))
			for _, n := range items {
				rows = append(rows, notificationRow(n))
			}
			return a.print(cmd, output.View{Data: items, Columns: notificationColumns, Rows: rows, Empty: "No notifications yet."})
		}),
	}
	list.Flags().StringVar(&ofBucket, "bucket", "", "only notifications of this bucket")
	cmd.AddCommand(list)

	var events []string
	var prefix, suffix, queue, topic, routingKey string
	create := &cobra.Command{
		Use:   "create <bucket> (--queue <queue> | --topic <topic>)",
		Short: "Add a notification to a bucket",
		Long: `Send a message to a queue or a topic when objects in a bucket are created
or removed. The message has the shape of an S3 event notification.

Delivery is at least once: a message can arrive twice with the same message
id, and messages about different keys can arrive out of order. Objects that
already exist are not announced.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := exactlyOne("queue", queue, "topic", topic); err != nil {
				return err
			}
			if routingKey != "" && topic == "" {
				return usagef("--routing-key only applies with --topic")
			}
			kinds := make([]string, 0, len(events))
			for _, event := range events {
				kind := strings.ToUpper(strings.TrimSpace(event))
				if kind != "CREATED" && kind != "REMOVED" {
					return usagef("--event is created or removed, got %q", event)
				}
				kinds = append(kinds, kind)
			}
			api, err := a.client(false)
			if err != nil {
				return err
			}
			n, err := api.CreateBucketNotification(cmd.Context(), platform.BucketNotificationInput{
				Bucket: args[0], Events: kinds, KeyPrefix: prefix, KeySuffix: suffix, Queue: queue, Topic: topic, RoutingKey: routingKey,
			})
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: n, Columns: notificationColumns, Rows: [][]string{notificationRow(*n)}})
		},
	}
	create.Flags().StringSliceVar(&events, "event", []string{"created"}, "what to announce: created, removed (repeatable or comma-separated)")
	create.Flags().StringVar(&prefix, "prefix", "", "only keys that start with this")
	create.Flags().StringVar(&suffix, "suffix", "", "only keys that end with this")
	create.Flags().StringVar(&queue, "queue", "", "send the messages into this queue")
	create.Flags().StringVar(&topic, "topic", "", "publish the messages to this topic")
	create.Flags().StringVar(&routingKey, "routing-key", "", "with --topic: routing key of the messages")
	cmd.AddCommand(create)

	cmd.AddCommand(a.deleteCommand("delete <bucket> <notification-id>", "Remove a notification from a bucket",
		"Remove notification %[2]s from bucket %[1]s?", "Removed notification %[2]s from bucket %[1]s.",
		a.sdk(func(cmd *cobra.Command, api *platform.Client, args []string) error {
			return api.DeleteBucketNotification(cmd.Context(), args[0], args[1])
		})))
	return cmd
}
