package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/golang/glog"
	"github.com/spf13/pflag"
	extclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/helm/pkg/helm/environment"

	helmClientset "github.com/fengxsong/helm-crd/pkg/client/clientset/versioned"
	informers "github.com/fengxsong/helm-crd/pkg/client/informers/externalversions"
)

var (
	settings environment.EnvSettings
)

func init() {
	settings.AddFlags(pflag.CommandLine)
}

func start() error {
	config, err := rest.InClusterConfig()
	if err != nil {
		return err
	}

	kubeClientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}
	clientset, err := helmClientset.NewForConfig(config)
	if err != nil {
		return err
	}
	extClient, err := extclientset.NewForConfig(config)
	if err != nil {
		return err
	}
	if err := ensureResource(extClient); err != nil {
		return err
	}
	crdInformersFactory := informers.NewSharedInformerFactory(clientset, time.Second*60)
	controller := NewController(kubeClientset, clientset, crdInformersFactory)
	stopCh := make(chan struct{})
	defer close(stopCh)
	go crdInformersFactory.Start(stopCh)
	go controller.Run(stopCh)

	sigterm := make(chan os.Signal, 1)
	signal.Notify(sigterm, syscall.SIGTERM)
	<-sigterm

	return nil
}

func main() {
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	flag.CommandLine.Parse([]string{})
	pflag.Parse()

	settings.Init(pflag.CommandLine)
	if err := start(); err != nil {
		glog.Fatal(err.Error())
	}
}
