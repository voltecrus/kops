package main

import (
	"fmt"
	"github.com/golang/glog"
	"github.com/spf13/cobra"
	"k8s.io/kops/upup/pkg/api"
	"k8s.io/kops/upup/pkg/fi"
	"k8s.io/kops/upup/pkg/fi/cloudup"
	"k8s.io/kops/util/pkg/tables"
	k8sapi "k8s.io/kubernetes/pkg/api"
	"os"
)

type UpgradeClusterCmd struct {
	Yes bool

	Channel string
}

var upgradeCluster UpgradeClusterCmd

func init() {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Upgrade cluster",
		Long:  `Upgrades a k8s cluster.`,
		Run: func(cmd *cobra.Command, args []string) {
			err := upgradeCluster.Run(args)
			if err != nil {
				exitWithError(err)
			}
		},
	}

	cmd.Flags().BoolVar(&upgradeCluster.Yes, "yes", false, "Apply update")
	cmd.Flags().StringVar(&upgradeCluster.Channel, "channel", "", "Channel to use for upgrade")

	upgradeCmd.AddCommand(cmd)
}

type upgradeAction struct {
	Item     string
	Property string
	Old      string
	New      string

	apply func()
}

func (c *UpgradeClusterCmd) Run(args []string) error {
	err := rootCommand.ProcessArgs(args)
	if err != nil {
		return err
	}

	cluster, err := rootCommand.Cluster()
	if err != nil {
		return err
	}

	clientset, err := rootCommand.Clientset()
	if err != nil {
		return err
	}

	list, err := clientset.InstanceGroups(cluster.Name).List(k8sapi.ListOptions{})
	if err != nil {
		return err
	}

	var instanceGroups []*api.InstanceGroup
	for i := range list.Items {
		instanceGroups = append(instanceGroups, &list.Items[i])
	}

	if cluster.Annotations[api.AnnotationNameManagement] == api.AnnotationValueManagementImported {
		return fmt.Errorf("upgrade is not for use with imported clusters (did you mean `kops toolbox convert-imported`?)")
	}

	channelLocation := c.Channel
	if channelLocation == "" {
		channelLocation = cluster.Spec.Channel
	}
	if channelLocation == "" {
		channelLocation = api.DefaultChannel
	}

	var actions []*upgradeAction
	if channelLocation != cluster.Spec.Channel {
		actions = append(actions, &upgradeAction{
			Item:     "Cluster",
			Property: "Channel",
			Old:      cluster.Spec.Channel,
			New:      channelLocation,
			apply: func() {
				cluster.Spec.Channel = channelLocation
			},
		})
	}

	channel, err := api.LoadChannel(channelLocation)
	if err != nil {
		return fmt.Errorf("error loading channel %q: %v", channelLocation, err)
	}

	channelClusterSpec := channel.Spec.Cluster
	if channelClusterSpec == nil {
		// Just to prevent too much nil handling
		channelClusterSpec = &api.ClusterSpec{}
	}

	//latestKubernetesVersion, err := api.FindLatestKubernetesVersion()
	//if err != nil {
	//	return err
	//}

	if channelClusterSpec.KubernetesVersion != "" && cluster.Spec.KubernetesVersion != channelClusterSpec.KubernetesVersion {
		actions = append(actions, &upgradeAction{
			Item:     "Cluster",
			Property: "KubernetesVersion",
			Old:      cluster.Spec.KubernetesVersion,
			New:      channelClusterSpec.KubernetesVersion,
			apply: func() {
				cluster.Spec.KubernetesVersion = channelClusterSpec.KubernetesVersion
			},
		})
	}

	// Prompt to upgrade addins?

	// Prompt to upgrade to kubenet
	if channelClusterSpec.Networking != nil {
		clusterNetworking := cluster.Spec.Networking
		if clusterNetworking == nil {
			clusterNetworking = &api.NetworkingSpec{}
		}
		// TODO: make this less hard coded
		if channelClusterSpec.Networking.Kubenet != nil && channelClusterSpec.Networking.Classic != nil {
			actions = append(actions, &upgradeAction{
				Item:     "Cluster",
				Property: "Networking",
				Old:      "classic",
				New:      "kubenet",
				apply: func() {
					cluster.Spec.Networking.Classic = nil
					cluster.Spec.Networking.Kubenet = channelClusterSpec.Networking.Kubenet
				},
			})
		}
	}

	cloud, err := cloudup.BuildCloud(cluster)
	if err != nil {
		return err
	}

	// Prompt to upgrade image
	{
		image := channel.FindImage(cloud.ProviderID())

		if image == nil {
			glog.Warningf("No matching images specified in channel; cannot prompt for upgrade")
		} else {
			for _, ig := range instanceGroups {
				if ig.Spec.Image != image.Name {
					target := ig
					actions = append(actions, &upgradeAction{
						Item:     "InstanceGroup/" + target.Name,
						Property: "Image",
						Old:      target.Spec.Image,
						New:      image.Name,
						apply: func() {
							target.Spec.Image = image.Name
						},
					})
				}
			}
		}
	}

	// Prompt to upgrade to overlayfs
	if channelClusterSpec.Docker != nil {
		if cluster.Spec.Docker == nil {
			cluster.Spec.Docker = &api.DockerConfig{}
		}
		// TODO: make less hard-coded
		if channelClusterSpec.Docker.Storage != nil {
			dockerStorage := fi.StringValue(cluster.Spec.Docker.Storage)
			if dockerStorage != fi.StringValue(channelClusterSpec.Docker.Storage) {
				actions = append(actions, &upgradeAction{
					Item:     "Cluster",
					Property: "Docker.Storage",
					Old:      dockerStorage,
					New:      fi.StringValue(channelClusterSpec.Docker.Storage),
					apply: func() {
						cluster.Spec.Docker.Storage = channelClusterSpec.Docker.Storage
					},
				})
			}
		}
	}

	if len(actions) == 0 {
		// TODO: Allow --force option to force even if not needed?
		// Note stderr - we try not to print to stdout if no update is needed
		fmt.Fprintf(os.Stderr, "\nNo upgrade required\n")
		return nil
	}

	{
		t := &tables.Table{}
		t.AddColumn("ITEM", func(a *upgradeAction) string {
			return a.Item
		})
		t.AddColumn("PROPERTY", func(a *upgradeAction) string {
			return a.Property
		})
		t.AddColumn("OLD", func(a *upgradeAction) string {
			return a.Old
		})
		t.AddColumn("NEW", func(a *upgradeAction) string {
			return a.New
		})

		err := t.Render(actions, os.Stdout, "ITEM", "PROPERTY", "OLD", "NEW")
		if err != nil {
			return err
		}
	}

	if !c.Yes {
		fmt.Printf("\nMust specify --yes to perform upgrade\n")
		return nil
	} else {
		for _, action := range actions {
			action.apply()
		}

		// TODO: DRY this chunk
		err = cluster.PerformAssignments()
		if err != nil {
			return fmt.Errorf("error populating configuration: %v", err)
		}

		fullCluster, err := cloudup.PopulateClusterSpec(cluster)
		if err != nil {
			return err
		}

		err = api.DeepValidate(fullCluster, instanceGroups, true)
		if err != nil {
			return err
		}

		// Note we perform as much validation as we can, before writing a bad config
		_, err = clientset.Clusters().Update(cluster)
		if err != nil {
			return err
		}

		for _, g := range instanceGroups {
			_, err := clientset.InstanceGroups(cluster.Name).Update(g)
			if err != nil {
				return fmt.Errorf("error writing InstanceGroup %q: %v", g.Name, err)
			}
		}

		fmt.Printf("\nUpdates applied to configuration.\n")

		// TODO: automate this step
		fmt.Printf("You can now apply these changes, using `kops update cluster %s`\n", cluster.Name)
	}

	return nil
}
