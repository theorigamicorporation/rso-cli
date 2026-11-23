package cli

import (
	"context"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/theorigamicorporation/rso-cli/internal/output"
	"github.com/theorigamicorporation/rsodev-go/platform"
)

var instanceColumns = []string{"ID", "NAME", "STATUS", "CPUS", "MEMORY", "DISK"}

func instanceRow(i platform.Instance) []string {
	cpus, memory, disk := "", "", ""
	if i.CPUs != nil {
		cpus = strconv.Itoa(*i.CPUs)
	}
	if i.MemoryBytes != nil {
		memory = bytesize(*i.MemoryBytes)
	}
	if i.DiskBytes != nil {
		disk = bytesize(*i.DiskBytes)
	}
	return []string{strconv.Itoa(i.ID), deref(i.Name), deref(i.Status), cpus, memory, disk}
}

// findInstance takes an instance by its numeric id or by its name.
func findInstance(ctx context.Context, client *platform.Client, ref string) (*platform.Instance, error) {
	if id, err := strconv.Atoi(ref); err == nil {
		return client.GetInstance(ctx, id)
	}
	instance, err := client.FindInstanceByName(ctx, ref)
	if err != nil {
		return nil, err
	}
	if instance == nil {
		return nil, notFoundf("Instance %s not found", ref)
	}
	return instance, nil
}

// instanceAction is a command that does one thing to one instance.
func (a *app) instanceAction(use, short, done string, act func(ctx context.Context, client *platform.Client, id int) error) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			instance, err := findInstance(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			if err := act(cmd.Context(), client, instance.ID); err != nil {
				return err
			}
			a.note(cmd, done, first(deref(instance.Name), args[0]))
			return nil
		}),
	}
}

func (a *app) instanceCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "instance", Aliases: []string{"instances"}, Short: "Manage virtual machine instances"}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List instances",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, _ []string) error {
			instances, err := client.ListInstances(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(instances))
			for _, i := range instances {
				rows = append(rows, instanceRow(i))
			}
			return a.print(cmd, output.View{Data: instances, Columns: instanceColumns, Rows: rows, Empty: "No instances yet."})
		}),
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get <instance>",
		Short: "Show an instance, by name or id",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			found, err := findInstance(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			// The lookup by name does not carry the addresses.
			instance, err := client.GetInstance(cmd.Context(), found.ID)
			if err != nil {
				return err
			}
			row := instanceRow(*instance)
			fields := [][2]string{{"ID", row[0]}, {"Name", row[1]}, {"Status", row[2]}, {"CPUs", row[3]}, {"Memory", row[4]}, {"Disk", row[5]}}
			if instance.Interfaces != nil {
				for _, nic := range instance.Interfaces.Items {
					addresses := make([]string, 0, len(nic.IPAddresses))
					for _, ip := range nic.IPAddresses {
						addresses = append(addresses, ip.Address)
					}
					fields = append(fields, [2]string{"Address (" + nic.Name + ")", strings.Join(addresses, ", ")})
				}
			}
			return a.print(cmd, output.View{Data: instance, Fields: fields})
		}),
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "templates",
		Short: "List the images an instance can be created from",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, _ []string) error {
			templates, err := client.ListInstanceTemplates(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(templates))
			for _, t := range templates {
				rows = append(rows, []string{t.ID, t.Name, deref(t.Description)})
			}
			return a.print(cmd, output.View{Data: templates, Columns: []string{"ID", "NAME", "DESCRIPTION"}, Rows: rows, Empty: "No templates."})
		}),
	})

	var in platform.CreateInstanceInput
	var description string
	var noWait bool
	create := &cobra.Command{
		Use:   "create <name> --template <template>",
		Short: "Create an instance from a template",
		Long: `Create a virtual machine instance from a template (see "rso instance
templates"). SSH keys of the tenant's key store are put on it with --ssh-key
(see "rso ssh-key list").

The command waits until the instance exists; --no-wait returns at once with
the id of the background task.`,
		Args: cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			if in.Template == "" {
				return usagef(`--template is required: see "rso instance templates"`)
			}
			in.Name = args[0]
			if description != "" {
				in.Description = &description
			}
			if noWait {
				task, err := client.CreateInstance(cmd.Context(), in)
				if err != nil {
					return err
				}
				return a.print(cmd, output.View{Data: map[string]string{"task": task}, Fields: [][2]string{{"Task", task}}})
			}
			a.note(cmd, "Creating instance %s...", in.Name)
			instance, err := client.CreateInstanceAndWait(cmd.Context(), in)
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: instance, Columns: instanceColumns, Rows: [][]string{instanceRow(*instance)}})
		}),
	}
	flags := create.Flags()
	flags.StringVar(&in.Template, "template", "", "template to create the instance from")
	flags.IntVar(&in.CPUs, "cpus", 2, "number of virtual CPUs")
	flags.IntVar(&in.MemoryMB, "memory", 2048, "memory in MB")
	flags.IntVar(&in.RootDiskGB, "disk", 0, "size of the root disk in GB (default: the template's)")
	flags.StringSliceVar(&in.SSHKeyIDs, "ssh-key", nil, "id of a key in the tenant's key store (repeatable)")
	flags.StringVar(&description, "description", "", "what the instance is for")
	flags.BoolVar(&noWait, "no-wait", false, "return at once with the id of the background task")
	cmd.AddCommand(create)

	cmd.AddCommand(
		a.instanceAction("start <instance>", "Start an instance", "Starting instance %s.", func(ctx context.Context, client *platform.Client, id int) error {
			return client.StartInstance(ctx, id)
		}),
		a.instanceAction("stop <instance>", "Stop an instance", "Stopping instance %s.", func(ctx context.Context, client *platform.Client, id int) error {
			return client.StopInstance(ctx, id)
		}),
		a.instanceAction("reboot <instance>", "Reboot an instance", "Rebooting instance %s.", func(ctx context.Context, client *platform.Client, id int) error {
			return client.RebootInstance(ctx, id)
		}),
	)

	cmd.AddCommand(a.deleteCommand("delete <instance>", "Delete an instance and its disks",
		"Delete instance %s and its disks?", "Deleted instance %s.",
		a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			instance, err := findInstance(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			return client.DeleteInstance(cmd.Context(), instance.ID)
		})))
	return cmd
}
