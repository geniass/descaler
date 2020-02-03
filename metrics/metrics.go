package metrics

import (
	"encoding/json"
	"strings"

	"k8s.io/client-go/kubernetes"

	autoscaling_v2beta1 "k8s.io/api/autoscaling/v2beta1"
	custom_metrics_v1beta1 "k8s.io/metrics/pkg/apis/custom_metrics/v1beta1"
	external_metrics_v1beta1 "k8s.io/metrics/pkg/apis/external_metrics/v1beta1"
)

func GetExternalMetric(clientset *kubernetes.Clientset, namespace string, metricSpec *autoscaling_v2beta1.ExternalMetricSource) (metrics *external_metrics_v1beta1.ExternalMetricValueList, err error) {
	req := clientset.RESTClient().Get().RequestURI("/apis/external.metrics.k8s.io/v1beta1/namespaces/" + namespace + "/" + metricSpec.MetricName)

	labels := []string{}
	for label, value := range metricSpec.MetricSelector.MatchLabels {
		labels = append(labels, label+"="+value)
	}
	req.Param("labelSelector", strings.Join(labels, ","))

	data, err := req.DoRaw()
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(data, &metrics)
	return metrics, err
}

func ListExternalMetrics(clientset *kubernetes.Clientset) (metrics *external_metrics_v1beta1.ExternalMetricValueList, err error) {
	data, err := clientset.RESTClient().Get().AbsPath("apis/external.metrics.k8s.io/v1beta1/namespaces/default").DoRaw()
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(data, &metrics)
	return metrics, err
}

func ListCustomMetrics(clientset *kubernetes.Clientset) (metrics *custom_metrics_v1beta1.MetricValueList, err error) {
	data, err := clientset.RESTClient().Get().AbsPath("apis/custom.metrics.k8s.io/v1beta1/").DoRaw()
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(data, &metrics)
	return metrics, err
}
