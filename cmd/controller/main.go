package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/fengxsong/helm-crd/pkg/controller"
	"github.com/golang/glog"
)

func main() {
	stopCh := make(chan struct{})
	defer close(stopCh)

	c, err := controller.NewController()
	if err != nil {
		glog.Fatal(err.Error())
	}
	go c.Run(stopCh)

	sigterm := make(chan os.Signal, 1)
	signal.Notify(sigterm, syscall.SIGTERM)
	<-sigterm
}
