package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/kops/upup/pkg/api"
	"k8s.io/kops/util/pkg/tables"
	k8sapi "k8s.io/kubernetes/pkg/api"
)

type GetInstanceGroupsCmd struct {
}

var getInstanceGroupsCmd GetInstanceGroupsCmd

func init() {
	cmd := &cobra.Command{
		Use:     "instancegroups",
		Aliases: []string{"instancegroup", "ig"},
		Short:   "get instancegroups",
		Long:    `List or get InstanceGroups.`,
		Run: func(cmd *cobra.Command, args []string) {
			err := getInstanceGroupsCmd.Run(args)
			if err != nil {
				exitWithError(err)
			}
		},
	}

	getCmd.cobraCommand.AddCommand(cmd)
}

func (c *GetInstanceGroupsCmd) Run(args []string) error {
	clusterName := rootCommand.ClusterName()
	if clusterName == "" {
		return fmt.Errorf("--name is required")
	}

	clientset, err := rootCommand.Clientset()
	if err != nil {
		return err
	}

	list, err := clientset.InstanceGroups(clusterName).List(k8sapi.ListOptions{})
	if err != nil {
		return err
	}

	var instancegroups []*api.InstanceGroup
	if len(args) != 0 {
		m := make(map[string]*api.InstanceGroup)
		for i := range list.Items {
			ig := &list.Items[i]
			m[ig.Name] = ig
		}
		instancegroups = make([]*api.InstanceGroup, 0, len(args))
		for _, arg := range args {
			ig := m[arg]
			if ig == nil {
				return fmt.Errorf("instancegroup not found %q", arg)
			}

			instancegroups = append(instancegroups, ig)
		}
	} else {
		for i := range list.Items {
			ig := &list.Items[i]
			instancegroups = append(instancegroups, ig)
		}
	}

	if len(instancegroups) == 0 {
		fmt.Fprintf(os.Stderr, "No InstanceGroup objects found\n")
		return nil
	}

	output := getCmd.output
	if output == OutputTable {
		t := &tables.Table{}
		t.AddColumn("NAME", func(c *api.InstanceGroup) string {
			return c.Name
		})
		t.AddColumn("ROLE", func(c *api.InstanceGroup) string {
			return string(c.Spec.Role)
		})
		t.AddColumn("MACHINETYPE", func(c *api.InstanceGroup) string {
			return c.Spec.MachineType
		})
		t.AddColumn("ZONES", func(c *api.InstanceGroup) string {
			return strings.Join(c.Spec.Zones, ",")
		})
		t.AddColumn("MIN", func(c *api.InstanceGroup) string {
			return intPointerToString(c.Spec.MinSize)
		})
		t.AddColumn("MAX", func(c *api.InstanceGroup) string {
			return intPointerToString(c.Spec.MinSize)
		})
		return t.Render(instancegroups, os.Stdout, "NAME", "ROLE", "MACHINETYPE", "MIN", "MAX", "ZONES")
	} else if output == OutputYaml {
		for _, ig := range instancegroups {
			y, err := api.ToYaml(ig)
			if err != nil {
				return fmt.Errorf("error marshaling yaml for %q: %v", ig.Name, err)
			}
			_, err = os.Stdout.Write(y)
			if err != nil {
				return fmt.Errorf("error writing to stdout: %v", err)
			}
		}
		return nil
	} else {
		return fmt.Errorf("Unknown output format: %q", output)
	}
}

func intPointerToString(v *int) string {
	if v == nil {
		return "-"
	}
	return strconv.Itoa(*v)
}
