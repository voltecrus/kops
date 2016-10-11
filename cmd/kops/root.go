package main

import (
	goflag "flag"
	"fmt"
	"github.com/golang/glog"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"k8s.io/kops/pkg/client/simple"
	"k8s.io/kops/pkg/client/simple/vfsclientset"
	"k8s.io/kops/upup/pkg/api"
	"k8s.io/kops/upup/pkg/kutil"
	"k8s.io/kops/util/pkg/vfs"
	"os"
)

type RootCmd struct {
	configFile string

	clientset simple.Clientset

	stateLocation string
	clusterName   string

	cobraCommand *cobra.Command
}

var rootCommand = RootCmd{
	cobraCommand: &cobra.Command{
		Use:   "kops",
		Short: "kops is kubernetes ops",
		Long: `kops is kubernetes ops.
It allows you to create, destroy, upgrade and maintain clusters.`,
	},
}

func Execute() {
	goflag.Set("logtostderr", "true")
	goflag.CommandLine.Parse([]string{})
	if err := rootCommand.cobraCommand.Execute(); err != nil {
		exitWithError(err)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	cmd := rootCommand.cobraCommand

	cmd.PersistentFlags().AddGoFlagSet(goflag.CommandLine)

	cmd.PersistentFlags().StringVar(&rootCommand.configFile, "config", "", "config file (default is $HOME/.kops.yaml)")

	defaultStateStore := os.Getenv("KOPS_STATE_STORE")
	cmd.PersistentFlags().StringVarP(&rootCommand.stateLocation, "state", "", defaultStateStore, "Location of state storage")

	cmd.PersistentFlags().StringVarP(&rootCommand.clusterName, "name", "", "", "Name of cluster")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if rootCommand.configFile != "" {
		// enable ability to specify config file via flag
		viper.SetConfigFile(rootCommand.configFile)
	}

	viper.SetConfigName(".kops") // name of config file (without extension)
	viper.AddConfigPath("$HOME") // adding home directory as first search path
	viper.AutomaticEnv()         // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("Using config file:", viper.ConfigFileUsed())
	}
}

func (c *RootCmd) AddCommand(cmd *cobra.Command) {
	c.cobraCommand.AddCommand(cmd)
}

// ProcessArgs will parse the positional args.  It assumes one of these formats:
//  * <no arguments at all>
//  * <clustername> (and --name not specified)
// Everything else is an error.
func (c *RootCmd) ProcessArgs(args []string) error {
	if len(args) == 0 {
		return nil
	}
	if len(args) == 1 {
		// Assume <clustername>
		if c.clusterName != "" {
			return fmt.Errorf("Cannot specify cluster via --name and positional argument")
		}
		c.clusterName = args[0]
		return nil
	}

	return fmt.Errorf("expected a single <clustername> to be passed as an argument")
}

func (c *RootCmd) ClusterName() string {
	if c.clusterName != "" {
		return c.clusterName
	}

	config, err := readKubectlClusterConfig()
	if err != nil {
		glog.Warningf("error reading kubecfg: %v", err)
	} else if config != nil && config.Name != "" {
		fmt.Fprintf(os.Stderr, "Using cluster from kubectl context: %s\n\n", config.Name)
		c.clusterName = config.Name
	}
	return c.clusterName
}

func readKubectlClusterConfig() (*kutil.KubectlClusterWithName, error) {
	kubectl := &kutil.Kubectl{}
	context, err := kubectl.GetCurrentContext()
	if err != nil {
		return nil, fmt.Errorf("error getting current context from kubectl: %v", err)
	}
	glog.V(4).Infof("context = %q", context)

	config, err := kubectl.GetConfig(true)
	if err != nil {
		return nil, fmt.Errorf("error getting current config from kubectl: %v", err)
	}

	// Minify should have done this
	if len(config.Clusters) != 1 {
		return nil, fmt.Errorf("expected exactly one cluster in kubectl config, found %d", len(config.Clusters))
	}

	return config.Clusters[0], nil
}

func (c *RootCmd) Clientset() (simple.Clientset, error) {
	basePath, err := vfs.Context.BuildVfsPath(c.stateLocation)
	if err != nil {
		return nil, fmt.Errorf("error building state store path for %q: %v", c.stateLocation, err)
	}

	if !vfs.IsClusterReadable(basePath) {
		return nil, fmt.Errorf("State store %q is not cloud-reachable - please use an S3 bucket", c.stateLocation)
	}

	clientset := vfsclientset.NewVFSClientset(basePath)

	return clientset, nil
}

func (c *RootCmd) Cluster() (*api.Cluster, error) {
	clientset, err := c.Clientset()
	if err != nil {
		return nil, err
	}

	clusterName := c.ClusterName()
	if clusterName == "" {
		return nil, fmt.Errorf("--name is required")
	}

	cluster, err := clientset.Clusters().Get(clusterName)
	if err != nil {
		return nil, fmt.Errorf("error reading cluster configuration: %v", err)
	}
	if cluster == nil {
		return nil, fmt.Errorf("cluster %q not found", clusterName)
	}

	if clusterName != cluster.Name {
		return nil, fmt.Errorf("cluster name did not match expected name: %v vs %v", clusterName, cluster.Name)
	}
	return cluster, nil
}
