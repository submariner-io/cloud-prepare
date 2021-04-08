package aws

import (
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

	publicSubnet, err := ac.getTaggedPublicSubnet(vpcID)
	if err != nil {
		return err
	}
	if publicSubnet == nil {
		publicSubnet, err = ac.getPublicSubnet(vpcID)
		if err != nil {
			return err
		}

		publicSubnetName := extractName(publicSubnet.Tags)
		reporter.Started("Adjusting public subnet %s to support Submariner", publicSubnetName)
		err = ac.tagPublicSubnet(publicSubnet.SubnetId)
		if err != nil {
			reporter.Failed(err)
			return err
		}
		reporter.Succeeded("Adjusted public subnet %s to support Submariner", publicSubnetName)
	}

	reporter.Started("Deploying gateway node")
	err = ac.deployGateway(vpcID, gatewaySG, publicSubnet)
	if err != nil {
		reporter.Failed(err)
		return err
	}
	reporter.Succeeded("Deployed gateway node")
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

	publicSubnet, err := ac.getTaggedPublicSubnet(vpcID)
	if err != nil {
		return err
	}
	if publicSubnet != nil {
		reporter.Started("Removing gateway node")
		err = ac.deleteGateway(vpcID, publicSubnet)
		if err != nil {
			reporter.Failed(err)
			return err
		}
		reporter.Succeeded("Removed gateway node")

		publicSubnetName := extractName(publicSubnet.Tags)
		reporter.Started("Untagging public subnet %s from supporting Submariner", publicSubnetName)
		err = ac.untagPublicSubnet(publicSubnet.SubnetId)
		if err != nil {
			reporter.Failed(err)
			return err
		}
		reporter.Succeeded("Untagged public subnet %s from supporting Submariner", publicSubnetName)
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
