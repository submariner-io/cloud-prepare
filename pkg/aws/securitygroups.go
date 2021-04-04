package aws

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/submariner-io/cloud-prepare/pkg/api"
)

const internalTraffic = "Internal Submariner traffic"

func (ac *awsCloud) getSecurityGroupID(vpcID string, name string) (*string, error) {
	filters := []*ec2.Filter{
		ec2Filter("vpc-id", vpcID),
		ac.filterByName(name),
		ac.filterByCurrentCluster(),
	}

	result, err := ac.client.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
		Filters: filters,
	})

	if err != nil {
		return nil, err
	}

	if len(result.SecurityGroups) == 0 {
		return nil, newNotFoundError("security group %s", name)
	}

	return result.SecurityGroups[0].GroupId, nil
}

func (ac *awsCloud) authorizeSecurityGroupIngress(groupID *string, ipPermissions []*ec2.IpPermission) error {
	input := &ec2.AuthorizeSecurityGroupIngressInput{
		GroupId:       groupID,
		IpPermissions: ipPermissions,
	}

	_, err := ac.client.AuthorizeSecurityGroupIngress(input)
	if awsErr, ok := err.(awserr.Error); ok {
		// Has to be hardcoded, see https://github.com/aws/aws-sdk-go/issues/3235
		if awsErr.Code() == "InvalidPermission.Duplicate" {
			return nil
		}
	}

	return err
}

func (ac *awsCloud) createClusterSGRule(srcGroup, destGroup *string, port uint16, protocol string, description string) error {
	ipPermissions := []*ec2.IpPermission{
		{
			FromPort:   aws.Int64(int64(port)),
			ToPort:     aws.Int64(int64(port)),
			IpProtocol: aws.String(protocol),
			UserIdGroupPairs: []*ec2.UserIdGroupPair{
				{
					Description: aws.String(description),
					GroupId:     srcGroup,
				},
			},
		},
	}

	return ac.authorizeSecurityGroupIngress(destGroup, ipPermissions)
}

func (ac *awsCloud) allowPortInCluster(vpcID string, port uint16, protocol string) error {
	workerGroupID, err := ac.getSecurityGroupID(vpcID, "{infraID}-worker-sg")
	if err != nil {
		return err
	}

	masterGroupID, err := ac.getSecurityGroupID(vpcID, "{infraID}-master-sg")
	if err != nil {
		return err
	}

	err = ac.createClusterSGRule(workerGroupID, workerGroupID, port, protocol, fmt.Sprintf("%s between the workers", internalTraffic))
	if err != nil {
		return err
	}

	err = ac.createClusterSGRule(workerGroupID, masterGroupID, port, protocol, fmt.Sprintf("%s from worker to master nodes", internalTraffic))
	if err != nil {
		return err
	}

	return ac.createClusterSGRule(masterGroupID, workerGroupID, port, protocol, fmt.Sprintf("%s from master to worker nodes", internalTraffic))
}

func (ac *awsCloud) createPublicSGRule(groupID *string, port uint16, protocol string, description string) error {
	ipPermissions := []*ec2.IpPermission{
		{
			FromPort:   aws.Int64(int64(port)),
			ToPort:     aws.Int64(int64(port)),
			IpProtocol: aws.String(protocol),
			IpRanges: []*ec2.IpRange{
				{
					CidrIp:      aws.String("0.0.0.0/0"),
					Description: aws.String(description),
				},
			},
		},
	}

	return ac.authorizeSecurityGroupIngress(groupID, ipPermissions)
}

func (ac *awsCloud) createGatewaySG(vpcID string, ports []api.PortSpec) (string, error) {
	groupName := ac.withAWSInfo("{infraID}-submariner-gw-sg")
	gatewayGroupID, err := ac.getSecurityGroupID(vpcID, groupName)
	if err != nil {
		if !isNotFoundError(err) {
			return "", err
		}

		input := &ec2.CreateSecurityGroupInput{
			GroupName:   &groupName,
			Description: aws.String("Submariner Gateway"),
			VpcId:       &vpcID,
			TagSpecifications: []*ec2.TagSpecification{
				{
					ResourceType: aws.String("security-group"),
					Tags: []*ec2.Tag{
						ec2Tag("Name", groupName),
						ec2Tag(ac.withAWSInfo("kubernetes.io/cluster/{infraID}"), "owned"),
					},
				},
			},
		}

		result, err := ac.client.CreateSecurityGroup(input)
		if err != nil {
			return "", err
		}

		gatewayGroupID = result.GroupId
	}

	for _, port := range ports {
		err = ac.createPublicSGRule(gatewayGroupID, port.Port, port.Protocol, "Public Submariner traffic")
		if err != nil {
			return "", err
		}
	}

	return groupName, nil
}
