module k8s.isazi.ai/descaler

go 1.13

require (
	github.com/bitnami-labs/kubewatch v0.0.4 // indirect
	github.com/sirupsen/logrus v1.4.2
	k8s.io/api v0.17.2
	k8s.io/apimachinery v0.17.2
	k8s.io/client-go v0.17.2
	k8s.io/metrics v0.17.2
)

replace (
	golang.org/x/sys => golang.org/x/sys v0.0.0-20190813064441-fde4db37ae7a // pinned to release-branch.go1.13
	golang.org/x/tools => golang.org/x/tools v0.0.0-20190821162956-65e3620a7ae7 // pinned to release-branch.go1.13
	k8s.io/api => k8s.io/api v0.0.0-20200124032216-924612ff3bca
	k8s.io/apimachinery => k8s.io/apimachinery v0.0.0-20200124032037-954b62493c18
	k8s.io/client-go => k8s.io/client-go v0.0.0-20200124112438-142dce433b42
	k8s.io/code-generator => k8s.io/code-generator v0.0.0-20200124111904-efe2535de926
)
