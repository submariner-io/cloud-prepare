# Submariner cloud-prepare

<!-- markdownlint-disable line-length -->
[![CII Best Practices](https://bestpractices.coreinfrastructure.org/projects/4865/badge)](https://bestpractices.coreinfrastructure.org/projects/4865)
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

### GCP

In order to prepare a GCP instance, it needs to have OpenShift pre-installed and running.

```go
	import (
		"golang.org/x/oauth2/google"
		dns "google.golang.org/api/dns/v1"
		gcpclient "github.com/submariner-io/cloud-prepare/pkg/gcp/client"
		cloudpreparegcp "github.com/submariner-io/cloud-prepare/pkg/gcp"
	)

	// Create Google credentials from a JSON value.
	// The JSON can represent either a Google Developers Console client_credentials.json file (as in ConfigFromJSON)
	// or a Google Developers service account key file (as in JWTConfigFromJSON).
	credentials, err := google.CredentialsFromJSON(context.TODO(), authJSON, dns.CloudPlatformScope)
	if err != nil {
		t.Fatal(err)
	}

	// Create a GCP client with the credentials.
	client, err := gcpclient.NewClient([]option.ClientOption{option.WithCredentials(credentials)})
	if err != nil {
		return err
	}

	// Create a new Cloud with the GCP client and the projectID of the credentials, infraID is necessary to properly deploy on GCP.
	cloud := cloudpreparegcp.NewCloud(credentials.ProjectID, infraID, client)
```
