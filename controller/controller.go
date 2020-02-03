/*
Copyright 2016 Skippbox, Ltd.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"

	"k8s.isazi.ai/descaler/event"
	"k8s.isazi.ai/descaler/handlers"
	"k8s.isazi.ai/descaler/metrics"
	"k8s.isazi.ai/descaler/utils"

	autoscaling_v2beta1 "k8s.io/api/autoscaling/v2beta1"

	"k8s.io/apimachinery/pkg/api/resource"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
)

const maxRetries = 5

// these constants define the Descaler's behaviour
// there's a trade-off between responsiveness (small scaleUpWaitPeriod) and cost-efficiency (high scaleUpWaitPeriod)
// the Descaler will wait at least scaleDownWaitPeriod to scale down after scaling up
// and it will wait at least scaleUpWaitPeriod to check the queue length (i.e. scale up to 1) after scaling down
const scaleUpWaitPeriod = time.Minute * 2
const scaleDownWaitPeriod = time.Second * 30

var serverStartTime time.Time

// Event indicate the informerEvent
type Event struct {
	key          string
	eventType    string
	namespace    string
	resourceType string
}

// Controller object
type Controller struct {
	logger       *logrus.Entry
	clientset    *kubernetes.Clientset
	queue        workqueue.RateLimitingInterface
	informer     cache.SharedIndexInformer
	eventHandler handlers.Handler
}

// Start the controller that monitors Deployments
func Start(eventHandler handlers.Handler) {

	// configure client connection
	var kubeClient *kubernetes.Clientset
	_, err := rest.InClusterConfig()
	if err != nil {
		kubeClient = utils.GetClientOutOfCluster()
	} else {
		kubeClient = utils.GetClient()
	}

	// HPA informer
	{
		informer := cache.NewSharedIndexInformer(
			&cache.ListWatch{
				ListFunc: func(options meta_v1.ListOptions) (runtime.Object, error) {
					return kubeClient.AutoscalingV2beta1().HorizontalPodAutoscalers("default").List(options)
				},
				WatchFunc: func(options meta_v1.ListOptions) (watch.Interface, error) {
					return kubeClient.AutoscalingV2beta1().HorizontalPodAutoscalers("default").Watch(options)
				},
			},
			&autoscaling_v2beta1.HorizontalPodAutoscaler{},
			time.Second*20, //Skip resync
			cache.Indexers{},
		)

		c := newResourceController(kubeClient, eventHandler, informer, "horizontalpodautoscaler")
		stopCh := make(chan struct{})
		defer close(stopCh)
		go c.Run(stopCh)
	}

	sigterm := make(chan os.Signal, 1)
	signal.Notify(sigterm, syscall.SIGTERM)
	signal.Notify(sigterm, syscall.SIGINT)
	<-sigterm
}

func newResourceController(client *kubernetes.Clientset, eventHandler handlers.Handler, informer cache.SharedIndexInformer, resourceType string) *Controller {
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	var newEvent Event
	var err error
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			newEvent.key, err = cache.MetaNamespaceKeyFunc(obj)
			newEvent.eventType = "create"
			newEvent.resourceType = resourceType
			logrus.WithField("pkg", "kubewatch-"+resourceType).Infof("Processing add to %v: %s", resourceType, newEvent.key)
			if err == nil {
				queue.Add(newEvent)
			}
		},
		UpdateFunc: func(old, new interface{}) {
			newEvent.key, err = cache.MetaNamespaceKeyFunc(old)
			newEvent.eventType = "update"
			newEvent.resourceType = resourceType
			// logrus.WithField("pkg", "kubewatch-"+resourceType).Infof("Processing update to %v: %s", resourceType, newEvent.key)

			if err == nil {
				queue.Add(newEvent)
			}
		},
		DeleteFunc: func(obj interface{}) {
			newEvent.key, err = cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			newEvent.eventType = "delete"
			newEvent.resourceType = resourceType
			newEvent.namespace = utils.GetObjectMetaData(obj).Namespace
			logrus.WithField("pkg", "kubewatch-"+resourceType).Infof("Processing delete to %v: %s", resourceType, newEvent.key)
			if err == nil {
				queue.Add(newEvent)
			}
		},
	})

	return &Controller{
		logger:       logrus.WithField("pkg", "kubewatch-"+resourceType),
		clientset:    client,
		informer:     informer,
		queue:        queue,
		eventHandler: eventHandler,
	}
}

// Run starts the kubewatch controller
func (c *Controller) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	c.logger.Info("Starting kubewatch controller")
	serverStartTime = time.Now().Local()

	go c.informer.Run(stopCh)

	if !cache.WaitForCacheSync(stopCh, c.HasSynced) {
		utilruntime.HandleError(fmt.Errorf("Timed out waiting for caches to sync"))
		return
	}

	c.logger.Info("Kubewatch controller synced and ready")

	wait.Until(c.runWorker, time.Second, stopCh)
}

// HasSynced is required for the cache.Controller interface.
func (c *Controller) HasSynced() bool {
	return c.informer.HasSynced()
}

// LastSyncResourceVersion is required for the cache.Controller interface.
func (c *Controller) LastSyncResourceVersion() string {
	return c.informer.LastSyncResourceVersion()
}

func (c *Controller) runWorker() {
	for c.processNextItem() {
		// continue looping
	}
}

func (c *Controller) processNextItem() bool {
	newEvent, quit := c.queue.Get()

	if quit {
		return false
	}
	defer c.queue.Done(newEvent)
	err := c.processItem(newEvent.(Event))
	if err == nil {
		// No error, reset the ratelimit counters
		c.queue.Forget(newEvent)
	} else if c.queue.NumRequeues(newEvent) < maxRetries {
		c.logger.Errorf("Error processing %s (will retry): %v", newEvent.(Event).key, err)
		c.queue.AddRateLimited(newEvent)
	} else {
		// err != nil and too many retries
		c.logger.Errorf("Error processing %s (giving up): %v", newEvent.(Event).key, err)
		c.queue.Forget(newEvent)
		utilruntime.HandleError(err)
	}

	return true
}

/* TODOs
- Enhance event creation using client-side cacheing machanisms - pending
- Enhance the processItem to classify events - done
- Send alerts correspoding to events - done
*/

func (c *Controller) processItem(newEvent Event) error {
	obj, _, err := c.informer.GetIndexer().GetByKey(newEvent.key)
	if err != nil {
		return fmt.Errorf("Error fetching object with key %s from store: %v", newEvent.key, err)
	}
	// get object's metedata
	objectMeta := utils.GetObjectMetaData(obj)

	// process events based on its type
	switch newEvent.eventType {
	case "create":
		// compare CreationTimestamp and serverStartTime and alert only on latest events
		// Could be Replaced by using Delta or DeltaFIFO
		if objectMeta.CreationTimestamp.Sub(serverStartTime).Seconds() > 0 {
			if newEvent.resourceType == "horizontalpodautoscaler" {
				autoscaler := obj.(*autoscaling_v2beta1.HorizontalPodAutoscaler)
				err := hpaUpdated(c.clientset, autoscaler)
				if err != nil {
					panic(err)
				}
			}
			c.eventHandler.ObjectCreated(obj)
			return nil
		}
	case "update":
		/* TODOs
		- enahace update event processing in such a way that, it send alerts about what got changed.
		*/
		if newEvent.resourceType == "horizontalpodautoscaler" {
			autoscaler := obj.(*autoscaling_v2beta1.HorizontalPodAutoscaler)
			err := hpaUpdated(c.clientset, autoscaler)
			if err != nil {
				panic(err)
			}
		}
		// kbEvent := event.Event{
		// 	Kind: newEvent.resourceType,
		// 	Name: newEvent.key,
		// }
		// c.eventHandler.ObjectUpdated(obj, kbEvent)

		return nil
	case "delete":
		kbEvent := event.Event{
			Kind:      newEvent.resourceType,
			Name:      newEvent.key,
			Namespace: newEvent.namespace,
		}
		c.eventHandler.ObjectDeleted(kbEvent)
		return nil
	}
	return nil
}

func hpaUpdated(clientSet *kubernetes.Clientset, autoscaler *autoscaling_v2beta1.HorizontalPodAutoscaler) error {
	replicas := autoscaler.Status.CurrentReplicas
	// targetKind := autoscaler.Spec.ScaleTargetRef.Kind
	target := autoscaler.Spec.ScaleTargetRef.Name

	var metricSpec *autoscaling_v2beta1.ExternalMetricSource
	for _, ems := range autoscaler.Spec.Metrics {
		if ems.Type == autoscaling_v2beta1.ExternalMetricSourceType {
			metricSpec = ems.External
			break
		}
	}
	if metricSpec == nil {
		// TODO: return an identifiable error
		//return errors.New("HPA does not use an External metric")
		return nil
	}

	logger := logrus.WithFields(logrus.Fields{
		"name":    metricSpec.MetricName,
		"queue":   metricSpec.MetricSelector.MatchLabels["metric.labels.queue"],
		"cluster": metricSpec.MetricSelector.MatchLabels["resource.labels.cluster_name"],
	})
	logger.Infof("External metric found in spec")

	m, err := metrics.GetExternalMetric(clientSet, "default", metricSpec)
	if err != nil {
		panic(err)
	}

	var metricValue resource.Quantity
	for _, mv := range m.Items {
		if mv.MetricLabels["resource.labels.cluster_name"] != metricSpec.MetricSelector.MatchLabels["resource.labels.cluster_name"] {
			logger.Warnf("Got metric for wrong cluster: %s", mv.MetricLabels["resource.labels.cluster_name"])
			continue
		}

		metricValue = mv.Value
	}
	logger.Infof("External metric value: %s", metricValue.String())

	// Metrics are alive!

	if replicas > 0 && metricValue.IsZero() {
		// 0 things in the queue, so scale to 0
		// TODO: maybe have a cooldown period before scaling to 0 in case more work comes in (although the HPA already does that...)
		//       have to store the last time we saw 0 things in the queue as an annotation; then compute time since then
		// TODO: this might become an annotation on a custom resource at some point

		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			// Retrieve the latest version of Deployment before attempting update
			// RetryOnConflict uses exponential backoff to avoid exhausting the apiserver
			deployment, err := clientSet.AppsV1().Deployments("default").Get(target, meta_v1.GetOptions{})
			if err != nil {
				return err
			}

			// lastScaledDownTimeStr, ok := deployment.Annotations["k8s.isazi.ai/last-scaled-down"]
			lastScaledUpTimeStr, ok := deployment.Annotations["k8s.isazi.ai/last-scaled-up"]
			if ok {
				lastTime, err := time.Parse(time.RFC3339, lastScaledUpTimeStr)
				if err == nil {
					if since := (time.Now().UTC().Sub(lastTime)); since < scaleDownWaitPeriod {
						logrus.Infof("Not descaling (last scaled up %s ago)", since.String())
						return nil
					}
				}
			}

			logrus.WithField("name", autoscaler.Name).Infof("Scaling down!")
			deployment.Annotations["k8s.isazi.ai/last-scaled-down"] = time.Now().UTC().Format(time.RFC3339)
			var scale int32
			scale = 0
			deployment.Spec.Replicas = &scale
			deployment, err = clientSet.AppsV1().Deployments("default").Update(deployment)
			if err != nil {
				return err
			}

			logrus.WithField("name", autoscaler.Name).Infof("Scaled to 0!")
			return nil
		})
		if err != nil {
			return err
		}
	}
	if replicas == 0 && !metricValue.IsZero() {
		// TODO: scale up to 1 (or minReplicas actually)
		logrus.
			WithFields(logrus.Fields{
				"name": autoscaler.Name,
				// "targetKind": targetKind,
				"target":   target,
				"replicas": replicas,
				"value":    metricValue.String(),
			}).
			Info("Detected a Descaled HPA, maybe scaling up!")

		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			// Retrieve the latest version of Deployment before attempting update
			// RetryOnConflict uses exponential backoff to avoid exhausting the apiserver
			deployment, err := clientSet.AppsV1().Deployments("default").Get(target, meta_v1.GetOptions{})
			if err != nil {
				return err
			}

			deployment.Annotations["k8s.isazi.ai/last-scaled-up"] = time.Now().UTC().Format(time.RFC3339)
			var scale int32
			scale = 1
			deployment.Spec.Replicas = &scale
			deployment, err = clientSet.AppsV1().Deployments("default").Update(deployment)
			if err != nil {
				return err
			}

			logrus.WithField("name", autoscaler.Name).Infof("Scaled to 1!")
			return nil
		})
		if err != nil {
			return err
		}
	}

	return nil
}
