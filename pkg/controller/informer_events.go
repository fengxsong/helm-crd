package controller

import (
	"reflect"

	"github.com/golang/glog"
	"k8s.io/client-go/tools/cache"

	"github.com/fengxsong/helm-crd/pkg/apis/helm.bitnami.com/v1"
)

func (c *Controller) onAddFunc(obj interface{}) {
	hr := obj.(*v1.HelmRelease)
	switch hr.Status.Phase {
	case v1.HelmRealeasePhaseUnknown:
	default:
		glog.Infof("HelmRelease %s/%s is not new, skipping (phase=%q)", hr.Namespace, hr.Name, hr.Status.Phase)
		return
	}
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err == nil {
		c.queue.Add(key)
	}
}

func (c *Controller) onUpdateFunc(oldObj, newObj interface{}) {
	oldhr := oldObj.(*v1.HelmRelease)
	newhr := newObj.(*v1.HelmRelease)
	if oldhr.ResourceVersion == newhr.ResourceVersion {
		return
	}
	// TODO: deal with failure?
	if newhr.Status.Phase == v1.HelmRealeasePhaseFailed {
		glog.Infof("Skipping helmrelease %s (phase=%q)", newhr.Name, newhr.Status.Phase)
		return
	}
	if reflect.DeepEqual(newhr.Spec, oldhr.Spec) && newhr.Status.Phase == v1.HelmRealeasePhaseReady {
		return
	}
	key, err := cache.MetaNamespaceKeyFunc(newObj)
	if err == nil {
		c.queue.Add(key)
	}
}

func (c *Controller) onDeleteFunc(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err == nil {
		c.queue.Add(key)
	}
}
