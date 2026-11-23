package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/theorigamicorporation/rso-cli/internal/output"
	"github.com/theorigamicorporation/rsodev-go/platform"
)

var postgresColumns = []string{"NAME", "STATUS", "MEMBERS", "STORAGE", "CREATED"}

func postgresRow(c platform.PostgresCluster) []string {
	status, members := "", fmt.Sprintf("%d", c.Spec.Instances)
	if c.Status != nil {
		status = deref(c.Status.Phase)
		if c.Status.ReadyInstances != nil {
			members = fmt.Sprintf("%d/%d", *c.Status.ReadyInstances, c.Spec.Instances)
		}
	}
	return []string{c.Name, status, members, c.Spec.Storage.Size, when(deref(c.CreatedAt))}
}

func postgresFields(c *platform.PostgresCluster) [][2]string {
	row := postgresRow(*c)
	fields := [][2]string{{"Name", row[0]}, {"Status", row[1]}, {"Members", row[2]}, {"Storage", row[3]}, {"Created", row[4]}}
	if c.Status != nil {
		fields = append(fields, [2]string{"URL", deref(c.Status.URL)})
	}
	return fields
}

func (a *app) postgresCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "postgres", Aliases: []string{"pg"}, Short: "Manage PostgreSQL clusters, their databases and backups"}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List PostgreSQL clusters",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, _ []string) error {
			clusters, err := client.ListPostgresClusters(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(clusters))
			for _, c := range clusters {
				rows = append(rows, postgresRow(c))
			}
			return a.print(cmd, output.View{Data: clusters, Columns: postgresColumns, Rows: rows, Empty: "No PostgreSQL clusters yet."})
		}),
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get <cluster>",
		Short: "Show a PostgreSQL cluster",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			c, err := client.GetPostgresCluster(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: c, Fields: postgresFields(c)})
		}),
	})

	var spec platform.PostgresSpec
	var cpu, memory string
	var wait bool
	var timeout time.Duration
	create := &cobra.Command{
		Use:   "create <cluster>",
		Short: "Create a PostgreSQL cluster",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			if cpu != "" || memory != "" {
				limits := &platform.ResourceList{}
				if cpu != "" {
					limits.CPU = &cpu
				}
				if memory != "" {
					limits.Memory = &memory
				}
				spec.Resources = &platform.Resources{Limits: limits}
			}
			c, err := client.CreatePostgresCluster(cmd.Context(), platform.PostgresClusterInput{Name: args[0], Spec: spec})
			if err != nil {
				return err
			}
			if wait {
				a.note(cmd, "Waiting for cluster %s to be ready...", args[0])
				if c, err = client.WaitPostgresClusterReady(cmd.Context(), args[0], platform.WaitOptions{Timeout: timeout}); err != nil {
					return err
				}
			}
			return a.print(cmd, output.View{Data: c, Fields: postgresFields(c)})
		}),
	}
	create.Flags().IntVar(&spec.Instances, "members", 1, "number of members; more than one gives a standby")
	create.Flags().StringVar(&spec.Storage.Size, "storage", "10Gi", "disk of each member")
	create.Flags().StringVar(&cpu, "cpu", "", "CPU limit of each member, for example 1")
	create.Flags().StringVar(&memory, "memory", "", "memory limit of each member, for example 2Gi")
	create.Flags().BoolVar(&wait, "wait", false, "wait until the cluster is ready")
	create.Flags().DurationVar(&timeout, "timeout", 10*time.Minute, "with --wait: how long to wait")
	cmd.AddCommand(create)

	var yes, keepBackups bool
	remove := &cobra.Command{
		Use:   "delete <cluster>",
		Short: "Delete a PostgreSQL cluster and its data",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			question := fmt.Sprintf("Delete PostgreSQL cluster %s, its data and its backups?", args[0])
			if keepBackups {
				question = fmt.Sprintf("Delete PostgreSQL cluster %s and its data? Its backups are kept.", args[0])
			}
			if err := a.confirm(cmd, yes, question); err != nil {
				return err
			}
			if err := client.DeletePostgresCluster(cmd.Context(), args[0], keepBackups); err != nil {
				return err
			}
			a.note(cmd, "Deleted PostgreSQL cluster %s.", args[0])
			return nil
		}),
	}
	remove.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	remove.Flags().BoolVar(&keepBackups, "keep-backups", false, "keep the backups, so the cluster can be restored later")
	cmd.AddCommand(remove)

	cmd.AddCommand(a.postgresDatabaseCommand(), a.postgresBackupCommand())
	return cmd
}

func (a *app) postgresDatabaseCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "database", Aliases: []string{"databases", "db"}, Short: "Manage the databases of a PostgreSQL cluster"}

	cmd.AddCommand(&cobra.Command{
		Use:   "list <cluster>",
		Short: "List the databases of a cluster",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			databases, err := client.ListPostgresDatabases(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(databases))
			for _, d := range databases {
				rows = append(rows, []string{d.Name, d.Owner})
			}
			return a.print(cmd, output.View{Data: databases, Columns: []string{"NAME", "OWNER"}, Rows: rows, Empty: "No databases yet."})
		}),
	})

	var owner string
	create := &cobra.Command{
		Use:   "create <cluster> <database>",
		Short: "Create a database in a cluster",
		Args:  cobra.ExactArgs(2),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			d, err := client.CreatePostgresDatabase(cmd.Context(), platform.PostgresDatabase{Cluster: args[0], Name: args[1], Owner: owner})
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: d, Fields: [][2]string{{"Cluster", d.Cluster}, {"Name", d.Name}, {"Owner", d.Owner}}})
		}),
	}
	create.Flags().StringVar(&owner, "owner", "app", "role that owns the database")
	cmd.AddCommand(create)

	cmd.AddCommand(a.deleteCommand("delete <cluster> <database>", "Delete a database",
		"Delete database %[2]s of cluster %[1]s?", "Deleted database %[2]s of cluster %[1]s.",
		a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			return client.DeletePostgresDatabase(cmd.Context(), args[0], args[1])
		})))
	return cmd
}

func backupFields(b *platform.PostgresBackups) [][2]string {
	fields := [][2]string{{"Enabled", yesNo(b.Enabled)}, {"Retention", deref(b.Retention)}}
	if b.Schedule != nil {
		fields = append(fields, [2]string{"Schedule", b.Schedule.Schedule}, [2]string{"Suspended", yesNo(b.Schedule.Suspended)},
			[2]string{"Next backup", when(deref(b.Schedule.Next))})
	}
	if b.Window.From != nil || b.Window.To != nil {
		fields = append(fields, [2]string{"Restorable from", when(deref(b.Window.From))}, [2]string{"Restorable to", when(deref(b.Window.To))})
	}
	return fields
}

func (a *app) postgresBackupCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "backup", Aliases: []string{"backups"}, Short: "Back up a PostgreSQL cluster and restore it"}

	cmd.AddCommand(&cobra.Command{
		Use:   "list <cluster>",
		Short: "Show the backup settings and the backups of a cluster",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			b, err := client.GetPostgresBackups(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if !b.Enabled {
				a.note(cmd, `Backups are off: turn them on with "rso postgres backup enable %s".`, args[0])
			} else if b.Schedule != nil {
				a.note(cmd, "Backups run on schedule %s, kept for %s.", b.Schedule.Schedule, deref(b.Retention))
			}
			rows := make([][]string, 0, len(b.Backups))
			for _, backup := range b.Backups {
				rows = append(rows, []string{backup.Name, deref(backup.Phase), when(deref(backup.StartedAt)), when(deref(backup.StoppedAt)), deref(backup.Error)})
			}
			return a.print(cmd, output.View{Data: b, Columns: []string{"NAME", "STATUS", "STARTED", "FINISHED", "ERROR"}, Rows: rows, Empty: "No backups yet."})
		}),
	})

	var retention string
	enable := &cobra.Command{
		Use:   "enable <cluster>",
		Short: "Turn backups on for a cluster",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			b, err := client.EnablePostgresBackups(cmd.Context(), args[0], retention)
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: b, Fields: backupFields(b)})
		}),
	}
	enable.Flags().StringVar(&retention, "retention", "30d", "how long backups are kept")
	cmd.AddCommand(enable)

	var cron, scheduleRetention string
	var suspend bool
	schedule := &cobra.Command{
		Use:   "schedule <cluster> --cron <expression>",
		Short: "Set when a cluster is backed up",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			if cron == "" {
				return usagef("--cron is required, for example --cron '0 3 * * *'")
			}
			b, err := client.SetPostgresBackupSchedule(cmd.Context(), args[0], cron, scheduleRetention, suspend)
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: b, Fields: backupFields(b)})
		}),
	}
	schedule.Flags().StringVar(&cron, "cron", "", "when to back up, as a cron expression")
	schedule.Flags().StringVar(&scheduleRetention, "retention", "", "how long backups are kept (default: unchanged)")
	schedule.Flags().BoolVar(&suspend, "suspend", false, "keep the schedule but do not run it")
	cmd.AddCommand(schedule)

	cmd.AddCommand(&cobra.Command{
		Use:   "unschedule <cluster>",
		Short: "Stop backing up a cluster on a schedule",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			if err := client.RemovePostgresBackupSchedule(cmd.Context(), args[0]); err != nil {
				return err
			}
			a.note(cmd, "Removed the backup schedule of cluster %s.", args[0])
			return nil
		}),
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "now <cluster>",
		Short: "Start a backup now",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			backup, err := client.BackupPostgresCluster(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: backup, Fields: [][2]string{{"Name", backup.Name}, {"Status", deref(backup.Phase)}, {"Started", when(deref(backup.StartedAt))}}})
		}),
	})

	var in platform.RestorePostgresInput
	var backup, at string
	var members int
	restore := &cobra.Command{
		Use:   "restore <cluster> --to <new-cluster>",
		Short: "Create a new cluster from the backups of another",
		Long: `Restore the backups of a cluster into a new cluster; the source is left
as it is. Without --backup or --at the most recent state is restored.`,
		Args: cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			if in.Name == "" {
				return usagef("--to is required: the name of the new cluster")
			}
			if backup != "" && at != "" {
				return usagef("give at most one of --backup and --at")
			}
			in.SourceCluster = args[0]
			if backup != "" {
				in.Backup = &backup
			}
			if at != "" {
				in.TargetTime = &at
			}
			if cmd.Flags().Changed("members") {
				in.Instances = &members
			}
			c, err := client.RestorePostgresCluster(cmd.Context(), in)
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: c, Fields: postgresFields(c)})
		}),
	}
	restore.Flags().StringVar(&in.Name, "to", "", "name of the new cluster")
	restore.Flags().StringVar(&backup, "backup", "", "restore this backup")
	restore.Flags().StringVar(&at, "at", "", "restore the state at this time (RFC 3339)")
	restore.Flags().IntVar(&members, "members", 0, "number of members of the new cluster (default: the source's)")
	cmd.AddCommand(restore)
	return cmd
}
