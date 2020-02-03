package handlers

import (
	"github.com/sirupsen/logrus"
	autoscaling_v2beta1 "k8s.io/api/autoscaling/v2beta1"
)

// Handler is implemented by any handler.
// The Handle method is used to process event
type Handler interface {
	ObjectCreated(obj interface{})
	ObjectDeleted(obj interface{})
	ObjectUpdated(oldObj, newObj interface{})
	TestHandler()
}

type Default struct {
}

// ObjectCreated sends events on object creation
func (d *Default) ObjectCreated(obj interface{}) {

}

// ObjectDeleted sends events on object deletion
func (d *Default) ObjectDeleted(obj interface{}) {

}

// ObjectUpdated sends events on object updation
func (d *Default) ObjectUpdated(oldObj, newObj interface{}) {
	switch object := oldObj.(type) {
	case *autoscaling_v2beta1.HorizontalPodAutoscaler:
		autoscaler := object
		if len(autoscaler.Status.CurrentMetrics) > 0 && autoscaler.Status.CurrentMetrics[0].External != nil {
			// CurrentValue is always 0 for some reason
			// value := autoscaler.Status.CurrentMetrics[0].External.CurrentValue.AsDec()
			replicas := autoscaler.Status.CurrentReplicas
			desired := autoscaler.Status.DesiredReplicas
			avg := autoscaler.Status.CurrentMetrics[0].External.CurrentAverageValue
			targetKind := autoscaler.Spec.ScaleTargetRef.Kind
			target := autoscaler.Spec.ScaleTargetRef.Name
			logrus.
				WithFields(logrus.Fields{
					"name":       autoscaler.Name,
					"targetKind": targetKind,
					"target":     target,
					"replicas":   replicas,
					"avg":        avg,
					"desired":    desired,
				}).
				Info("HPA Updated")

			if replicas == 0 && desired > 0 {
				// TODO: scale up to 1 (or minReplicas actually)
				// HACK: the HPA completely breaks when you set replicas=0. So we'll have to scale to 1 periodically so the HPA can refresh
				//		 if there's still nothing in the queue, we will scale to 0 again after some time
				//       else the HPA will take over and scale as usual
				// TODO: store the last time we scaled to 0 as an annotation; then compute time since then
				logrus.WithField("name", autoscaler.Name).Infof("Scaling up!")
			} else if replicas > 0 && desired == 1 && avg.IsZero() {
				// TODO: scale to 0
				// TODO: maybe have a cooldown period before scaling to 0 in case more work comes in (although the HPA already does that...)
				//       have to store the last time we saw 0 things in the queue as an annotation; then compute time since then
				logrus.WithField("name", autoscaler.Name).Infof("Scaling down!")
			}
		} else if autoscaler.Name != "api" {
			logrus.Infof("Unknown HPA Updated (%s): %+v", autoscaler.Name, autoscaler.Status.CurrentMetrics)
		}
	}
}

// TestHandler tests the handler configurarion by sending test messages.
func (d *Default) TestHandler() {

}
