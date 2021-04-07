package aws

import (
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/submariner-io/cloud-prepare/pkg/api"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
)

const (
	messageRetrieveVPCID  = "Retrieving VPC ID"
	messageRetrievedVPCID = "Retrieved VPC ID %s"
)

// MachineSetDeployer can deploy and delete machinesets from OCP
type MachineSetDeployer interface {
	// Deploy makes sure to deploy the given machine set (creating or updating it)
	Deploy(machineSet *unstructured.Unstructured) error

	// Delete will remove the given machineset
	Delete(machineSet *unstructured.Unstructured) error
}

type k8sMachineSetDeployer struct {
	k8sConfig *rest.Config
}

// NewK8sMachinesetDeployer returns a MachineSetDeployer capable deploying directly to Kubernetes
func NewK8sMachinesetDeployer(k8sConfig *rest.Config) MachineSetDeployer {
	return &k8sMachineSetDeployer{k8sConfig: k8sConfig}
}

type awsCloud struct {
	client         ec2iface.EC2API
	gwDeployer     MachineSetDeployer
	gwInstanceType string
	infraID        string
	region         string
}

// NewCloud creates a new api.Cloud instance which can prepare AWS for Submariner to be deployed on it
func NewCloud(gwDeployer MachineSetDeployer, client ec2iface.EC2API, infraID, region, gwInstanceType string) api.Cloud {
	return &awsCloud{
		client:         client,
		gwDeployer:     gwDeployer,
		gwInstanceType: gwInstanceType,
		infraID:        infraID,
		region:         region,
	}
}

func (ac *awsCloud) PrepareForSubmariner(input api.PrepareForSubmarinerInput, reporter api.Reporter) error {
	reporter.Started(messageRetrieveVPCID)
	vpcID, err := ac.getVpcID()
	if err != nil {
		reporter.Failed(err)
		return err
	}
	reporter.Succeeded(messageRetrievedVPCID, vpcID)

	for _, port := range input.InternalPorts {
		reporter.Started("Opening port %v protocol %s for intra-cluster communications", port.Port, port.Protocol)
		err = ac.allowPortInCluster(vpcID, port.Port, port.Protocol)
		if err != nil {
			reporter.Failed(err)
			return err
		}
		reporter.Succeeded("Opened port %v protocol %s for intra-cluster communications", port.Port, port.Protocol)
	}

	reporter.Started("Creating Submariner gateway security group")
	gatewaySG, err := ac.createGatewaySG(vpcID, input.PublicPorts)
	if err != nil {
		reporter.Failed(err)
		return err
	}
	reporter.Succeeded("Created Submariner gateway security group %s", gatewaySG)

	subnets, err := ac.getPublicSubnets(vpcID)
	if err != nil {
		return err
	}

	subnetsCount := len(subnets)
	if subnetsCount == 0 {
		return errors.New(fmt.Sprintf("Found no public subnets to deploy Submariner gateway(s)"))
	}
	if input.Gateways > 0 && len(subnets) < input.Gateways {
		return errors.New(fmt.Sprintf("Not enough public subnets to deploy %v Submariner gateway(s)", input.Gateways))
	}

	taggedSubnets, untaggedSubnets := filterTaggedSubnets(subnets)
	for _, subnet := range untaggedSubnets {
		if input.Gateways > 0 && len(taggedSubnets) == input.Gateways {
			break
		}

		subnetName := extractName(subnet.Tags)
		reporter.Started("Adjusting public subnet %s to support Submariner", subnetName)
		err = ac.tagPublicSubnet(subnet.SubnetId)
		if err != nil {
			reporter.Failed(err)
			return err
		}
		taggedSubnets = append(taggedSubnets, subnet)
		reporter.Succeeded("Adjusted public subnet %s to support Submariner", subnetName)
	}

	for _, subnet := range taggedSubnets {
		subnetName := extractName(subnet.Tags)

		reporter.Started("Deploying gateway node for public subnet %s", subnetName)
		err = ac.deployGateway(vpcID, gatewaySG, subnet)
		if err != nil {
			reporter.Failed(err)
			return err
		}
		reporter.Succeeded("Deployed gateway node for public subnet %s", subnetName)
	}
	return nil
}

func (ac *awsCloud) CleanupAfterSubmariner(reporter api.Reporter) error {
	reporter.Started(messageRetrieveVPCID)
	vpcID, err := ac.getVpcID()
	if err != nil {
		reporter.Failed(err)
		return err
	}
	reporter.Succeeded(messageRetrievedVPCID, vpcID)

	subnets, err := ac.getTaggedPublicSubnets(vpcID)
	if err != nil {
		return err
	}

	for _, subnet := range subnets {
		subnetName := extractName(subnet.Tags)
		reporter.Started("Removing gateway node for public subnet %s", subnetName)
		err = ac.deleteGateway(vpcID, subnet)
		if err != nil {
			reporter.Failed(err)
			return err
		}
		reporter.Succeeded("Removed gateway node for public subnet %s", subnetName)

		reporter.Started("Untagging public subnet %s from supporting Submariner", subnetName)
		err = ac.untagPublicSubnet(subnet.SubnetId)
		if err != nil {
			reporter.Failed(err)
			return err
		}
		reporter.Succeeded("Untagged public subnet %s from supporting Submariner", subnetName)
	}

	reporter.Started("Revoking intra-cluster communication permissions")
	err = ac.revokePortsInCluster(vpcID)
	if err != nil {
		reporter.Failed(err)
		return err
	}
	reporter.Succeeded("Revoked intra-cluster communication permissions")

	reporter.Started("Deleting Submariner gateway security group")
	err = ac.deleteGatewaySG(vpcID)
	if err != nil {
		reporter.Failed(err)
		return err
	}
	reporter.Succeeded("Deleted Submariner gateway security group")

	return nil
}
