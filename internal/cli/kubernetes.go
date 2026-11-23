package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/theorigamicorporation/rso-cli/internal/output"
	"github.com/theorigamicorporation/rsodev-go/platform"
)

var kubernetesColumns = []string{"NAME", "STATUS", "VERSION", "CONTROL PLANE", "WORKERS", "CREATED"}

func kubernetesRow(k platform.KubernetesCluster) []string {
	workers, ready := 0, 0
	for _, pool := range k.Pools {
		workers += pool.Replicas
		ready += pool.ReadyReplicas
	}
	return []string{
		k.Name, k.Status, k.Version,
		fmt.Sprintf("%d/%d", k.ControlPlane.ReadyReplicas, k.ControlPlane.Replicas),
		fmt.Sprintf("%d/%d", ready, workers), when(deref(k.CreatedAt)),
	}
}

// clusterName takes the cluster from the argument or from --cluster; the
// flag is what a kubeconfig written by the platform passes.
func clusterName(args []string, flag string) (string, error) {
	switch {
	case len(args) == 1 && flag != "" && args[0] != flag:
		return "", usagef("the cluster was given twice: %q and --cluster=%q", args[0], flag)
	case len(args) == 1:
		return args[0], nil
	case flag != "":
		return flag, nil
	}
	return "", usagef("which cluster? give its name")
}

func (a *app) kubernetesCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "kubernetes", Aliases: []string{"k8s"}, Short: "Kubernetes clusters: list them and sign in to them"}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List Kubernetes clusters",
		Args:  cobra.NoArgs,
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, _ []string) error {
			clusters, err := client.ListKubernetesClusters(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(clusters))
			for _, k := range clusters {
				rows = append(rows, kubernetesRow(k))
			}
			return a.print(cmd, output.View{Data: clusters, Columns: kubernetesColumns, Rows: rows, Empty: "No Kubernetes clusters yet."})
		}),
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get <cluster>",
		Short: "Show a Kubernetes cluster",
		Args:  cobra.ExactArgs(1),
		RunE: a.sdk(func(cmd *cobra.Command, client *platform.Client, args []string) error {
			k, err := client.GetKubernetesCluster(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			fields := [][2]string{
				{"Name", k.Name}, {"Status", k.Status}, {"Reason", deref(k.StatusReason)}, {"Version", k.Version},
				{"Upgrading to", deref(k.TargetVersion)}, {"API endpoint", deref(k.Endpoint)},
				{"Control plane", fmt.Sprintf("%d/%d ready, %d vCPU, %d GB RAM, %d GB disk", k.ControlPlane.ReadyReplicas, k.ControlPlane.Replicas, k.ControlPlane.VCPU, k.ControlPlane.RAMGB, k.ControlPlane.DiskGB)},
			}
			for _, pool := range k.Pools {
				fields = append(fields, [2]string{"Pool " + pool.Name, fmt.Sprintf("%d/%d ready, %d vCPU, %d GB RAM, %d GB disk", pool.ReadyReplicas, pool.Replicas, pool.VCPU, pool.RAMGB, pool.DiskGB)})
			}
			return a.print(cmd, output.View{Data: k, Fields: fields})
		}),
	})

	var kubeconfigCluster string
	var session bool
	kubeconfig := &cobra.Command{
		Use:   "kubeconfig <cluster>",
		Short: "Print a kubeconfig for a cluster",
		Long: `Print a kubeconfig for a cluster.

By default it contains no secret: kubectl runs "rso kubernetes token" for a
fresh, short-lived credential whenever it needs one, so the file is safe to
keep. That needs rso on the PATH and a sign-in (or the service account
variables) wherever kubectl runs.

With --session the kubeconfig carries a token of its own that expires within
the hour and needs nothing else.

  rso kubernetes kubeconfig production > production.kubeconfig
  KUBECONFIG=production.kubeconfig kubectl get nodes`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := clusterName(args, kubeconfigCluster)
			if err != nil {
				return err
			}
			client, err := a.client(false)
			if err != nil {
				return err
			}
			kind := platform.KubeconfigExec
			if session {
				kind = platform.KubeconfigSession
			}
			text, err := client.Kubeconfig(cmd.Context(), name, kind)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), text)
			return err
		},
	}
	kubeconfig.Flags().StringVar(&kubeconfigCluster, "cluster", "", "cluster name (same as the argument)")
	kubeconfig.Flags().BoolVar(&session, "session", false, "carry a token that expires within the hour instead of running rso")
	cmd.AddCommand(kubeconfig)

	var tokenCluster string
	var ttl int
	token := &cobra.Command{
		Use:   "token <cluster>",
		Short: "Print a short-lived credential for kubectl",
		Long: `Print a short-lived credential for a cluster, as the ExecCredential
document kubectl expects from a credential helper.

This is what a cluster's kubeconfig runs; you rarely type it yourself. The
credential carries the rights your account has in the tenant at that moment.
It is kept in a private cache until a minute before it expires, because
kubectl asks once per command; set RSO_NO_CACHE to switch that off.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := clusterName(args, tokenCluster)
			if err != nil {
				return err
			}
			api, err := a.client(false)
			if err != nil {
				return err
			}
			credential, err := kubernetesCredential(cmd.Context(), api, name, ttl, a.credentialCache(api.Tenant(), name))
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), credential)
			return err
		},
	}
	token.Flags().StringVar(&tokenCluster, "cluster", "", "cluster name (same as the argument)")
	token.Flags().IntVar(&ttl, "ttl", 900, "lifetime of the credential in seconds, "+strconv.Itoa(600)+" to "+strconv.Itoa(3600))
	cmd.AddCommand(token)
	return cmd
}

type execCredential struct {
	Status struct {
		Token               string    `json:"token"`
		ExpirationTimestamp time.Time `json:"expirationTimestamp"`
	} `json:"status"`
}

// credentialCache is the file a cluster's credential is kept in between
// kubectl commands; empty means no cache.
func (a *app) credentialCache(tenant, cluster string) string {
	if a.env.Getenv("RSO_NO_CACHE") != "" {
		return ""
	}
	dir := a.env.Getenv("RSO_CACHE_DIR")
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(base, "rso", "kubernetes")
	}
	api := first(a.apiURL, a.env.Getenv("RSO_API_URL"))
	sum := sha256.Sum256([]byte(api + "\x00" + tenant + "\x00" + cluster))
	return filepath.Join(dir, hex.EncodeToString(sum[:8])+".json")
}

// kubernetesCredential returns an ExecCredential for the cluster, from the
// cache while it is good for another minute, else from the platform.
func kubernetesCredential(ctx context.Context, api *platform.Client, cluster string, ttl int, cache string) (string, error) {
	if cache != "" {
		if cached, err := os.ReadFile(cache); err == nil {
			var parsed execCredential
			if json.Unmarshal(cached, &parsed) == nil && parsed.Status.Token != "" &&
				time.Until(parsed.Status.ExpirationTimestamp) > time.Minute {
				return string(cached), nil
			}
		}
	}
	credential, err := api.ClusterToken(ctx, cluster, ttl)
	if err != nil {
		return "", err
	}
	var parsed execCredential
	if json.Unmarshal([]byte(credential), &parsed) != nil || parsed.Status.Token == "" {
		return "", errors.New("the platform returned no credential")
	}
	if cache != "" {
		// Best effort: a helper that cannot write its cache still signs in.
		if os.MkdirAll(filepath.Dir(cache), 0o700) == nil {
			_ = os.WriteFile(cache, []byte(credential), 0o600)
		}
	}
	return credential, nil
}
