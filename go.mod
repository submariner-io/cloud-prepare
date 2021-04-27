module github.com/submariner-io/cloud-prepare

go 1.13

require (
	github.com/aws/aws-sdk-go v1.38.26
	github.com/submariner-io/admiral v0.9.0-m2
	k8s.io/apimachinery v0.18.4
	k8s.io/client-go v0.18.4
)

// Pinned to kubernetes-1.17.0
replace (
	k8s.io/apimachinery => k8s.io/apimachinery v0.17.0
	k8s.io/client-go => k8s.io/client-go v0.17.0
)
