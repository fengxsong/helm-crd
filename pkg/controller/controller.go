package controller

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/golang/glog"
	"google.golang.org/grpc"
	extclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/downloader"
	"k8s.io/helm/pkg/getter"
	"k8s.io/helm/pkg/helm"
	"k8s.io/helm/pkg/proto/hapi/release"
	"k8s.io/helm/pkg/repo"

	"github.com/fengxsong/helm-crd/pkg/apis/helm.bitnami.com/v1"
	"github.com/fengxsong/helm-crd/pkg/client/clientset/versioned"
	informers "github.com/fengxsong/helm-crd/pkg/client/informers/externalversions"
)

const (
	controllerName = "HelmReleases-controller"
	maxRetries     = 5
)

// Controller is a cache.Controller for acting on Helm CRD objects
type Controller struct {
	kubeClientset kubernetes.Interface
	clientset     versioned.Interface
	helmClient    *helm.Client
	informer      cache.SharedIndexInformer
	queue         workqueue.RateLimitingInterface
}

// NewController creates a Controller
func NewController() (*Controller, error) {
	kubeClientset, err := kubernetes.NewForConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	clientset, err := versioned.NewForConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	extClientset, err := extclientset.NewForConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	if err = ensureCustomResource(extClientset); err != nil {
		return nil, err
	}

	crdInformersFactory := informers.NewSharedInformerFactory(clientset, time.Second*time.Duration(resyncDuration))

	glog.Infof("Using tiller host: %s", settings.TillerHost)

	c := &Controller{
		kubeClientset: kubeClientset,
		clientset:     clientset,
		helmClient:    helm.NewClient(helm.Host(settings.TillerHost), helm.ConnectTimeout(5)),
		informer:      crdInformersFactory.Helm().V1().HelmReleases().Informer(),
		queue:         workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), ""),
	}

	c.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.onAddFunc,
		UpdateFunc: c.onUpdateFunc,
		DeleteFunc: c.onDeleteFunc,
	})

	return c, nil
}

// HasSynced returns true once this controller has completed an
// initial resource listing
func (c *Controller) HasSynced() bool {
	return c.informer.HasSynced()
}

// LastSyncResourceVersion is the resource version observed when last
// synced with the underlying store. The value returned is not
// synchronized with access to the underlying store and is not
// thread-safe.
func (c *Controller) LastSyncResourceVersion() string {
	return c.informer.LastSyncResourceVersion()
}

// Run begins processing items, and will continue until a value is
// sent down stopCh.  It's an error to call Run more than once.  Run
// blocks; call via go.
func (c *Controller) Run(stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.queue.ShutDown()

	go c.informer.Run(stopCh)
	// Start the informer factories to begin populating the informer caches
	glog.Infof("Starting %s", controllerName)

	// Set up a helm home dir sufficient to fool the rest of helm
	// client code
	os.MkdirAll(settings.Home.Archive(), 0755)
	os.MkdirAll(settings.Home.Repository(), 0755)
	ioutil.WriteFile(settings.Home.RepositoryFile(),
		[]byte("apiVersion: v1\nrepositories: []"), 0644)

	// Wait for the caches to be synced before starting workers
	glog.Info("Waiting for informer caches to sync")

	if !cache.WaitForCacheSync(stopCh, c.HasSynced) {
		runtime.HandleError(fmt.Errorf("Timeout waiting for caches to sync"))
		return
	}
	glog.Info("Cache synchronised, starting main loop")

	wait.Until(c.runWorker, time.Second, stopCh)

	glog.Infof("Shutting down %s", controllerName)
}

func (c *Controller) runWorker() {
	for c.processNextItem() {
		// continue looping
	}
}

func (c *Controller) processNextItem() bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}

	defer c.queue.Done(key)

	// should we deal with non-string error?
	err := c.updateRelease(key.(string))
	if err == nil {
		c.queue.Forget(key)
	} else if c.queue.NumRequeues(key) < maxRetries {
		glog.Errorf("Error updating %s, will retry: %v", key, err)
		c.queue.AddRateLimited(key)
	} else {
		glog.Errorf("Error updating %s, giving up: %v", key, err)
		c.queue.Forget(key)
		if err, ok := err.(*wrapError); ok {
			c.handleWrapError(err)
		}
		runtime.HandleError(err)
	}
	return true
}

func releaseName(ns, name string) string {
	return fmt.Sprintf("%s-%s", ns, name)
}

func isNotFound(err error) bool {
	// Ideally this would be `grpc.Code(err) == codes.NotFound`,
	// but it seems helm doesn't return grpc codes
	return strings.Contains(grpc.ErrorDesc(err), "not found")
}

func (c *Controller) updateRelease(key string) error {
	obj, exists, err := c.informer.GetIndexer().GetByKey(key)
	if err != nil {
		return fmt.Errorf("error fetching object with key %s from store: %v", key, err)
	}

	if !exists {
		glog.Infof("HelmRelease %s has gone, uninstalling chart", key)
		ns, name, err := cache.SplitMetaNamespaceKey(key)
		if err != nil {
			return err
		}
		_, err = c.helmClient.DeleteRelease(
			releaseName(ns, name),
			helm.DeletePurge(true),
		)
		if err != nil {
			return err
		}
		return nil
	}

	helmObj := obj.(*v1.HelmRelease)
	if helmObj.Spec.Paused {
		glog.Infof("HelmRelease %s is not yet process", helmObj.Name)
		return nil
	}
	// FIXME: make configurable
	keyring := "/keyring/pubring.gpg"

	dl := downloader.ChartDownloader{
		HelmHome: settings.Home,
		Out:      os.Stdout,
		Keyring:  keyring,
		Getters:  getter.All(settings),
		Verify:   downloader.VerifyNever, // FIXME
	}

	repoURL := helmObj.Spec.RepoURL
	if repoURL == "" {
		repoURL = defaultRepoURL
	}

	// FIXME: Make configurable
	certFile := ""
	keyFile := ""
	caFile := ""
	chartURL, err := repo.FindChartInAuthRepoURL(repoURL, helmObj.Spec.Username, helmObj.Spec.Password, helmObj.Spec.ChartName, helmObj.Spec.Version, certFile, keyFile, caFile, getter.All(settings))
	if err != nil {
		return &wrapError{helmObj, err}
	}

	glog.Infof("Downloading %s ...", chartURL)
	fname, _, err := dl.DownloadTo(chartURL, helmObj.Spec.Version, settings.Home.Archive())
	if err != nil {
		return &wrapError{helmObj, err}
	}
	glog.Infof("Downloaded %s to %s", chartURL, fname)
	chartRequested, err := chartutil.LoadFile(fname) // fixme: just download to ram buf
	if err != nil {
		glog.Errorf("Error loading chart file: %v", err)
		return &wrapError{helmObj, err}
	}

	rlsName := releaseName(helmObj.Namespace, helmObj.Name)

	var rel *release.Release
	_, err = c.helmClient.ReleaseHistory(rlsName, helm.WithMaxHistory(1))
	if err != nil {
		if !isNotFound(err) {
			glog.Errorf("Error getting release history: %v", err)
			return &wrapError{helmObj, err}
		}
		glog.Infof("Installing release %s into namespace %s", rlsName, helmObj.Namespace)
		res, err := c.helmClient.InstallReleaseFromChart(
			chartRequested,
			helmObj.Namespace,
			helm.ValueOverrides([]byte(helmObj.Spec.RawValues)),
			helm.ReleaseName(rlsName),
		)
		if err != nil {
			return &wrapError{helmObj, err}
		}
		rel = res.GetRelease()
	} else {
		glog.Infof("Update release %s with options UpgradeForce(%v)/UpgradeRecreate(%v)",
			rlsName, helmObj.Spec.Force, helmObj.Spec.Recreate)
		res, err := c.helmClient.UpdateReleaseFromChart(
			rlsName,
			chartRequested,
			helm.UpdateValueOverrides([]byte(helmObj.Spec.RawValues)),
			helm.UpgradeForce(helmObj.Spec.Force),
			helm.UpgradeRecreate(helmObj.Spec.Recreate),
		)
		if err != nil {
			return &wrapError{helmObj, err}
		}
		rel = res.GetRelease()
	}

	status, err := c.helmClient.ReleaseStatus(rel.Name)
	if err == nil {
		glog.Infof("Installed/updated release %s, version %d (status %s)", rel.Name, rel.Version, status.Info.Status.Code)
	} else {
		glog.Warningf("Unable to fetch release status for %s: %v", rel.Name, err)
	}
	helmObjCopy := helmObj.DeepCopy()

	helmObjCopy.Status.ChartURL = chartURL
	helmObjCopy.Status.Revision = rel.GetVersion()
	helmObjCopy.Status.Phase = v1.HelmRealeasePhaseReady
	if _, err := c.clientset.HelmV1().HelmReleases(helmObjCopy.Namespace).Update(helmObjCopy); err != nil {
		return &wrapError{helmObj, err}
	}
	return nil
}
