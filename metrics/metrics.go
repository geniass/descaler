package metrics

import (
	"encoding/json"

	"k8s.io/client-go/kubernetes"

	custom_metrics_v1beta1 "k8s.io/metrics/pkg/apis/custom_metrics/v1beta1"
	external_metrics_v1beta1 "k8s.io/metrics/pkg/apis/external_metrics/v1beta1"
)

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
