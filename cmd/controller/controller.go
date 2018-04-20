package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/golang/glog"
	"google.golang.org/grpc"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	extclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	helmcrdv1 "github.com/fengxsong/helm-crd/pkg/apis/helm.bitnami.com/v1"
	helmClientset "github.com/fengxsong/helm-crd/pkg/client/clientset/versioned"
	informers "github.com/fengxsong/helm-crd/pkg/client/informers/externalversions"
)

const (
	controllerName = "HelmReleases-controller"
	defaultRepoURL = "https://kubernetes-charts.storage.googleapis.com"
	maxRetries     = 5
)

type Controller struct {
	kubeClientset kubernetes.Interface
	clientset     helmClientset.Interface
	helmClient    *helm.Client
	informer      cache.SharedIndexInformer
	queue         workqueue.RateLimitingInterface
}

func NewController(
	kubeClientset kubernetes.Interface,
	clientset helmClientset.Interface,
	crdInformersFactory informers.SharedInformerFactory) *Controller {

	crdInformer := crdInformersFactory.Helm().V1().HelmReleases()

	glog.Infof("Using tiller host: %s", settings.TillerHost)

	c := &Controller{
		kubeClientset: kubeClientset,
		clientset:     clientset,
		helmClient:    helm.NewClient(helm.Host(settings.TillerHost), helm.ConnectTimeout(5)),
		informer:      crdInformer.Informer(),
		queue:         workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), ""),
	}

	c.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.onAddFunc,
		UpdateFunc: c.onUpdateFunc,
		DeleteFunc: c.onDeleteFunc,
	})

	return c
}

func ensureResource(extClient extclientset.Interface) error {
	crd := &apiextensions.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "helmreleases." + helmcrdv1.SchemeGroupVersion.Group,
		},
		Spec: apiextensions.CustomResourceDefinitionSpec{
			Group:   helmcrdv1.SchemeGroupVersion.Group,
			Version: helmcrdv1.SchemeGroupVersion.Version,
			Scope:   apiextensions.NamespaceScoped,
			Names: apiextensions.CustomResourceDefinitionNames{
				Plural:     "helmreleases",
				Singular:   "helmrelease",
				Kind:       "HelmRelease",
				ListKind:   "HelmReleaseList",
				ShortNames: []string{"hrl"},
			},
		},
	}
	_, err := extClient.ApiextensionsV1beta1().CustomResourceDefinitions().Create(crd)
	if apierrors.IsAlreadyExists(err) {
		glog.Info("Skip the creation for CustomResourceDefinition HelmReleases because it has already been created")
		return nil
	}
	if err != nil {
		return err
	}
	glog.Info("Create CustomResourceDefinition HelmReleases successfully")
	return nil
}

func (c *Controller) onAddFunc(obj interface{}) {
	glog.V(2).Info("onAddFunc is invoked.")
	hr := obj.(*helmcrdv1.HelmRelease)
	switch hr.Status.Phase {
	case helmcrdv1.HelmRealeasePhaseUnknown, helmcrdv1.HelmRealeasePhaseNew:
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
	glog.V(2).Info("onUpdateFunc is invoked.")
	old := oldObj.(*helmcrdv1.HelmRelease)
	new := newObj.(*helmcrdv1.HelmRelease)
	if old.ResourceVersion == new.ResourceVersion {
		return
	}
	// TODO: deal with failure?
	if new.Status.Phase == helmcrdv1.HelmRealeasePhaseFailed {
		glog.Infof("Skipping helmrelease %s (phase=%q)", new.Name, new.Status.Phase)
		return
	}
	if reflect.DeepEqual(new.Spec, old.Spec) && new.Status.Phase == helmcrdv1.HelmRealeasePhaseReady {
		return
	}
	key, err := cache.MetaNamespaceKeyFunc(newObj)
	if err == nil {
		c.queue.Add(key)
	}
}

func (c *Controller) onDeleteFunc(obj interface{}) {
	glog.V(2).Info("onDeleteFunc is invoked.")
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err == nil {
		c.queue.Add(key)
	}
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
	helmObj := obj.(*helmcrdv1.HelmRelease)
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
		// FIXME: Make configurable
		repoURL = defaultRepoURL
	}

	certFile := ""
	keyFile := ""
	caFile := ""
	chartURL, err := repo.FindChartInRepoURL(repoURL, helmObj.Spec.ChartName, helmObj.Spec.Version, certFile, keyFile, caFile, getter.All(settings))
	if err != nil {
		return err
	}

	glog.Infof("Downloading %s ...", chartURL)
	fname, _, err := dl.DownloadTo(chartURL, helmObj.Spec.Version, settings.Home.Archive())
	if err != nil {
		return err
	}
	glog.Infof("Downloaded %s to %s", chartURL, fname)
	chartRequested, err := chartutil.LoadFile(fname) // fixme: just download to ram buf
	if err != nil {
		glog.Errorf("Error loading chart file: %v", err)
		return err
	}

	rlsName := releaseName(helmObj.Namespace, helmObj.Name)

	var rel *release.Release
	_, err = c.helmClient.ReleaseHistory(rlsName, helm.WithMaxHistory(1))
	if err != nil {
		if !isNotFound(err) {
			glog.Errorf("Error getting release history: %v", err)
			return err
		}
		glog.Infof("Installing release %s into namespace %s", rlsName, helmObj.Namespace)
		res, err := c.helmClient.InstallReleaseFromChart(
			chartRequested,
			helmObj.Namespace,
			helm.ValueOverrides([]byte(helmObj.Spec.Values)),
			helm.ReleaseName(rlsName),
		)
		if err != nil {
			return err
		}
		rel = res.GetRelease()
	} else {
		glog.Infof("Update release %s with options UpgradeForce(%v)/UpgradeRecreate(%v)",
			rlsName, helmObj.Spec.Force, helmObj.Spec.Recreate)
		res, err := c.helmClient.UpdateReleaseFromChart(
			rlsName,
			chartRequested,
			helm.UpdateValueOverrides([]byte(helmObj.Spec.Values)),
			helm.UpgradeForce(helmObj.Spec.Force),
			helm.UpgradeRecreate(helmObj.Spec.Recreate),
		)
		if err != nil {
			return err
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
	// Is it necessary to do this?
	helmObjCopy.Status.Config = rel.GetChart().GetValues().GetRaw()

	helmObjCopy.Status.Revision = rel.GetVersion()
	helmObjCopy.Status.Phase = helmcrdv1.HelmRealeasePhaseReady
	if _, err := c.clientset.HelmV1().HelmReleases(helmObjCopy.Namespace).Update(helmObjCopy); err != nil {
		return err
	}
	return nil
}
