# Submariner cloud-prepare

<!-- markdownlint-disable line-length -->
[![Linting](https://github.com/submariner-io/cloud-prepare/workflows/Linting/badge.svg)](https://github.com/submariner-io/cloud-prepare/actions?query=workflow%3ALinting)
<!-- markdownlint-enable line-length -->

Submariner's cloud-prepare is a Go library that provides API and capabilities for setting up cloud infrastructure in order to install
Submariner.

## API

The [main API](https://github.com/submariner-io/cloud-prepare/blob/devel/pkg/api/api.go) defines the capabilities for any `Cloud`:

* Preparing the cloud for setting up Submariner.
* Cleaning up the cloud after Submariner has been uninstalled.

These capabilities aim to be idempotent, so in case of failure or other necessity they are safe to re-run.

The API defines a `Reporter` type which has the capability to report on the latest operation performed in the cloud.

### Prepare a cloud for Submariner

Preparing a cloud for Submariner takes in number of gateways, internal ports for intra-cluster communications (Submariner components)
and public ports for inter-cluster communications (Submariner to Submariner).

```go
	input := api.PrepareForSubmarinerInput{
		InternalPorts: []api.PortSpec{
			{Port: vxlanPort, Protocol: "udp"},
			{Port: metricsPort, Protocol: "tcp"},
		},
		PublicPorts: []api.PortSpec{
			{Port: nattPort, Protocol: "udp"},
			{Port: natDiscoveryPort, Protocol: "udp"},
		},
		Gateways: gateways,
	}
	err := cloud.PrepareForSubmariner(input, reporter)

```

### Clean up a cloud after Submariner has been uninstalled

Cleanup doesn't take input as it finds all preparation work done by the library and reverses it.

```go
	err := cloud.CleanupAfterSubmariner(repoter)
```

## AWS

AWS is supported by the library. In order to prepare an AWS instance, it needs to have OpenShift pre-installed and running.

```go
	// The gwDeployer deploys the gateway and is pluggable.
	// This one deploys straight to K8s using MachineSet.
	gwDeployer := cloudprepareaws.NewK8sMachinesetDeployer(k8sConfig)

	// Create a new Cloud from an existing AWS session;
	// infraID, region and gwInstanceType are necessary to properly deploy on AWS.
	cloud := cloudprepareaws.NewCloud(
		gwDeployer, ec2.New(awsSession), infraID, region, gwInstanceType)
```
