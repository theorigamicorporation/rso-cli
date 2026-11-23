package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/theorigamicorporation/rso-cli/internal/output"
	"github.com/theorigamicorporation/rsodev-go/platform"
)

var dnsRecordColumns = []string{"ID", "NAME", "TYPE", "CONTENT", "UPDATED"}

func dnsRecordRow(r platform.DNSRecord) []string {
	return []string{r.ID, r.Name, r.Type, r.Content, when(r.UpdatedAt)}
}

func (a *app) dnsCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "dns", Short: "Manage DNS records in the zones of the tenant"}

	cmd.AddCommand(&cobra.Command{
		Use:   "zones",
		Short: "List the zones the tenant can put records in",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, _ []string) error {
			zones, err := client.ListDNSZones(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(zones))
			for _, z := range zones {
				rows = append(rows, []string{z.Name})
			}
			return a.print(cmd, output.View{Data: zones, Columns: []string{"ZONE"}, Rows: rows, Empty: "No zones are offered to this tenant."})
		}),
	})

	record := &cobra.Command{Use: "record", Aliases: []string{"records"}, Short: "Manage DNS records"}
	cmd.AddCommand(record)

	var zone string
	list := &cobra.Command{
		Use:   "list",
		Short: "List DNS records",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, _ []string) error {
			records, err := client.ListDNSRecords(cmd.Context(), zone)
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(records))
			for _, r := range records {
				rows = append(rows, dnsRecordRow(r))
			}
			return a.print(cmd, output.View{Data: records, Columns: dnsRecordColumns, Rows: rows, Empty: "No DNS records yet."})
		}),
	}
	list.Flags().StringVar(&zone, "zone", "", "only records of this zone")
	record.AddCommand(list)

	record.AddCommand(&cobra.Command{
		Use:   "get <record-id>",
		Short: "Show a DNS record",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			r, err := client.GetDNSRecord(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: r, Fields: [][2]string{
				{"ID", r.ID}, {"Zone", r.Zone}, {"Name", r.Name}, {"Type", r.Type}, {"Content", r.Content},
				{"Created", when(r.CreatedAt)}, {"Created by", deref(r.CreatedBy)}, {"Updated", when(r.UpdatedAt)},
			}})
		}),
	})

	var recordType string
	create := &cobra.Command{
		Use:   "create <zone> <name> <content>",
		Short: "Create a DNS record",
		Long: `Create a record in a zone. The name is the full name, inside the zone:

  rso dns record create example.com shop.example.com 203.0.113.10
  rso dns record create example.com www.example.com shop.example.com --type CNAME

A name belongs to the tenant that created its first record until the last
record there is deleted.`,
		Args: cobra.ExactArgs(3),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			kind := strings.ToUpper(recordType)
			switch kind {
			case platform.DNSRecordA, platform.DNSRecordAAAA, platform.DNSRecordCNAME, platform.DNSRecordTXT:
			default:
				return usagef("--type is A, AAAA, CNAME or TXT, got %q", recordType)
			}
			r, err := client.CreateDNSRecord(cmd.Context(), args[0], args[1], kind, args[2])
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: r, Columns: dnsRecordColumns, Rows: [][]string{dnsRecordRow(*r)}})
		}),
	}
	create.Flags().StringVar(&recordType, "type", "A", "record type: A, AAAA, CNAME or TXT")
	record.AddCommand(create)

	record.AddCommand(&cobra.Command{
		Use:   "update <record-id> <content>",
		Short: "Change what a DNS record answers",
		Args:  cobra.ExactArgs(2),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			r, err := client.UpdateDNSRecord(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			return a.print(cmd, output.View{Data: r, Columns: dnsRecordColumns, Rows: [][]string{dnsRecordRow(*r)}})
		}),
	})

	record.AddCommand(a.deleteCommand("delete <record-id>", "Delete a DNS record",
		"Delete DNS record %s?", "Deleted DNS record %s.",
		a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			return client.DeleteDNSRecord(cmd.Context(), args[0])
		})))
	return cmd
}
