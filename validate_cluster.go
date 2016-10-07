package main

import (
	// "fmt"
	"github.com/golang/glog"
	"github.com/spf13/cobra"
	//"io/ioutil"
	// "k8s.io/kops/upup/pkg/api"
	//"k8s.io/kops/upup/pkg/fi"
	//"k8s.io/kops/upup/pkg/fi/cloudup"
	//"k8s.io/kops/upup/pkg/fi/utils"
	//"k8s.io/kops/upup/pkg/kutil"
	//"k8s.io/kubernetes/pkg/util/sets"
	//"strings"
)

type validateClusersCmd struct {
}

var validateClusers validateClusersCmd

func init() {
	cmd := &cobra.Command{
		Use:   "clusters",
		Short: "validate clusters",
		Long:  `validates k8s clusters.`,
		Run: func(cmd *cobra.Command, args []string) {
			err := validateClusers.Run(args)
			if err != nil {
				exitWithError(err)
			}
		},
	}

	validateCmd.AddCommand(cmd)
}

func (c *validateClusersCmd) Run(args []string) error {

	clusterRegistry, err := rootCommand.ClusterRegistry()
	if err != nil {
		return err
	}

	// var clusters []*api.Cluster

	clusterNames := args
	if len(args) == 0 {
		clusterNames, err = clusterRegistry.List()
		if err != nil {
			return err
		}
	}

	for _, clusterName := range clusterNames {

	}

	return nil

}
