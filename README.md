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

The `PrepareForSubmarinerInput` function takes the number of gateways, the internal ports used for intra-cluster communication between
Submariner components, and the public ports used for inter-cluster communication between Submariner gateways.

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

The `CleanupAfterSubmariner` function reverses all the preparation work previously done by the library.

```go
	err := cloud.CleanupAfterSubmariner(reporter)
```

## Supported Cloud Providers

### AWS

In order to prepare an AWS instance, it needs to have OpenShift pre-installed and running.

```go
	// The gwDeployer deploys the gateway and is pluggable.
	// This one deploys straight to K8s using MachineSet.
	gwDeployer := cloudprepareaws.NewK8sMachinesetDeployer(k8sConfig)

	// Create a new Cloud from an existing AWS session;
	// infraID, region and gwInstanceType are necessary to properly deploy on AWS.
	cloud := cloudprepareaws.NewCloud(
		gwDeployer, ec2.New(awsSession), infraID, region, gwInstanceType)
```
