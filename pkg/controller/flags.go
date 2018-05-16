package controller

import (
	"flag"

	"github.com/golang/glog"
	"github.com/spf13/pflag"
	"k8s.io/client-go/rest"
	"k8s.io/helm/pkg/helm/environment"
)

var (
	defaultRepoURL string
	resyncDuration int64
	kubeconfig     *rest.Config
	settings       environment.EnvSettings
)

func init() {
	settings.AddFlags(pflag.CommandLine)

	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	flag.CommandLine.Parse([]string{})
	pflag.Lookup("logtostderr").Value.Set("true")
	settings.Init(pflag.CommandLine)

	pflag.StringVar(&defaultRepoURL, "defaultRepoURL", "https://kubernetes-charts.storage.googleapis.com", "default repository url")
	pflag.Int64Var(&resyncDuration, "resync", 300, "resync cache duration")
	pflag.Parse()

	var err error
	if kubeconfig, err = rest.InClusterConfig(); err != nil {
		glog.Fatal(err.Error())
	}
}
