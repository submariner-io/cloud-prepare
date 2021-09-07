module github.com/submariner-io/cloud-prepare

go 1.13

require (
	github.com/aws/aws-sdk-go v1.40.37
	github.com/pkg/errors v0.9.1
	github.com/submariner-io/admiral v0.10.0-rc1.0.20210824123934-19b8fb8b93bc
	google.golang.org/api v0.56.0
	k8s.io/api v0.19.10
	k8s.io/apimachinery v0.19.10
	k8s.io/client-go v0.19.10
)

// Pinned to kubernetes-1.19.10
replace (
	k8s.io/api => k8s.io/api v0.19.10
	k8s.io/apimachinery => k8s.io/apimachinery v0.19.10
	k8s.io/client-go => k8s.io/client-go v0.19.10
)
