module github.com/submariner-io/cloud-prepare

go 1.13

require (
	github.com/aws/aws-sdk-go-v2 v1.11.1
	github.com/aws/aws-sdk-go-v2/config v1.10.2
	github.com/aws/aws-sdk-go-v2/credentials v1.6.2
	github.com/aws/aws-sdk-go-v2/service/ec2 v1.23.0
	github.com/aws/smithy-go v1.9.0
	github.com/golang/mock v1.6.0
	github.com/onsi/ginkgo v1.16.5
	github.com/onsi/gomega v1.17.0
	github.com/pkg/errors v0.9.1
	github.com/submariner-io/admiral v0.12.0-m0
	golang.org/x/net v0.0.0-20210614182718-04defd469f4e // indirect
	google.golang.org/api v0.61.0
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
