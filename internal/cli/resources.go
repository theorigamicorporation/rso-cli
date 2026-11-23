package cli

import (
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/theorigamicorporation/rso-cli/internal/output"
	"github.com/theorigamicorporation/rsodev-go/platform"
)

var registryColumns = []string{"NAME", "ADDRESS", "STATUS", "CREATED"}

func registryRow(r platform.Registry) []string {
	return []string{r.Name, r.Domain, r.Status, when(deref(r.CreatedAt))}
}

func (a *app) registryCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "registry", Aliases: []string{"registries"}, Short: "Manage container registries and sign in to them"}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List container registries",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, _ []string) error {
			registries, err := client.ListRegistries(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(registries))
			for _, r := range registries {
				rows = append(rows, registryRow(r))
			}
			return a.print(cmd, output.View{Data: registries, Columns: registryColumns, Rows: rows, Empty: "No registries yet."})
		}),
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get <registry>",
		Short: "Show a container registry",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			r, err := client.GetRegistry(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: r, Columns: registryColumns, Rows: [][]string{registryRow(*r)}})
		}),
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "create <registry>",
		Short: "Create a container registry",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			r, err := client.CreateRegistry(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: r, Columns: registryColumns, Rows: [][]string{registryRow(*r)}})
		}),
	})

	var push bool
	var ttl time.Duration
	credentials := &cobra.Command{
		Use:   "credentials <registry>",
		Short: "Issue a short-lived sign-in for a registry",
		Long: `Issue a user name and a password for a registry. They expire on their own,
so they are safe to hand to a build:

  rso registry credentials images --push -o json

Read-only by default; --push allows uploading images.`,
		Args: cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			c, err := client.IssueRegistryCredentials(cmd.Context(), args[0], push, ttl)
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: c, Fields: [][2]string{
				{"Registry", c.Registry}, {"Username", c.Username}, {"Password", c.Password}, {"Expires", when(c.ExpiresAt)},
			}})
		}),
	}
	credentials.Flags().BoolVar(&push, "push", false, "allow uploading images, not only downloading")
	credentials.Flags().DurationVar(&ttl, "ttl", 0, "how long the sign-in lasts (default: the platform's)")
	cmd.AddCommand(credentials)

	cmd.AddCommand(a.deleteCommand("delete <registry>", "Delete a container registry and its images",
		"Delete registry %s and every image in it?", "Deleted registry %s.",
		a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			return client.DeleteRegistry(cmd.Context(), args[0])
		})))
	return cmd
}

func (a *app) sshKeyCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "ssh-key", Aliases: []string{"ssh-keys"}, Short: "Manage the SSH public keys put on new instances"}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List SSH keys",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, _ []string) error {
			keys, err := client.ListSSHKeys(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(keys))
			for _, k := range keys {
				rows = append(rows, []string{k.ID, k.Name, k.KeyType, k.Fingerprint, when(k.CreatedAt)})
			}
			return a.print(cmd, output.View{Data: keys, Columns: []string{"ID", "NAME", "TYPE", "FINGERPRINT", "CREATED"}, Rows: rows, Empty: "No SSH keys yet."})
		}),
	})

	var file string
	add := &cobra.Command{
		Use:   "add <name> [--file <public-key-file>]",
		Short: "Add a public key, from a file or standard input",
		Long: `Add an SSH public key to the tenant's key store.

  rso ssh-key add laptop --file ~/.ssh/id_ed25519.pub
  cat ~/.ssh/id_ed25519.pub | rso ssh-key add laptop`,
		Args: cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			var text string
			if file != "" {
				raw, err := os.ReadFile(file)
				if err != nil {
					return usageError{err}
				}
				text = string(raw)
			} else {
				var err error
				if text, err = a.readAll(); err != nil {
					return err
				}
			}
			if text = strings.TrimSpace(text); text == "" {
				return usagef("no public key given: use --file or standard input")
			}
			k, err := client.AddSSHKey(cmd.Context(), args[0], text)
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: k, Fields: [][2]string{{"ID", k.ID}, {"Name", k.Name}, {"Type", k.KeyType}, {"Fingerprint", k.Fingerprint}}})
		}),
	}
	add.Flags().StringVar(&file, "file", "", "file with the public key")
	cmd.AddCommand(add)

	cmd.AddCommand(a.deleteCommand("delete <key-id>", "Remove an SSH key from the key store",
		"Remove SSH key %s? Instances that already have it keep it.", "Removed SSH key %s.",
		a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			return client.DeleteSSHKey(cmd.Context(), args[0])
		})))
	return cmd
}

var volumeColumns = []string{"NAME", "SIZE", "STATUS", "USED BY", "CREATED"}

func volumeRow(v platform.Volume) []string {
	return []string{v.Name, count(v.SizeGB) + " GB", deref(v.Status), strings.Join(v.UsedBy, ","), when(deref(v.CreatedAt))}
}

func (a *app) volumeCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "volume", Aliases: []string{"volumes"}, Short: "Manage the volumes apps keep their data on"}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List volumes",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, _ []string) error {
			volumes, err := client.ListVolumes(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(volumes))
			for _, v := range volumes {
				rows = append(rows, volumeRow(v))
			}
			return a.print(cmd, output.View{Data: volumes, Columns: volumeColumns, Rows: rows, Empty: "No volumes yet."})
		}),
	})

	var size int
	create := &cobra.Command{
		Use:   "create <volume> --size <GB>",
		Short: "Create a volume",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			if size <= 0 {
				return usagef("--size is required: the size in GB")
			}
			v, err := client.CreateVolume(cmd.Context(), args[0], size)
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: v, Columns: volumeColumns, Rows: [][]string{volumeRow(*v)}})
		}),
	}
	create.Flags().IntVar(&size, "size", 0, "size in GB")
	cmd.AddCommand(create)

	cmd.AddCommand(a.deleteCommand("delete <volume>", "Delete a volume and its data",
		"Delete volume %s and its data?", "Deleted volume %s.",
		a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			return client.DeleteVolume(cmd.Context(), args[0])
		})))
	return cmd
}
