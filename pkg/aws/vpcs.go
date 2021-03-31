package aws

import "github.com/aws/aws-sdk-go/service/ec2"

func (ac *awsCloud) getVpcID() (string, error) {
	vpcName := ac.withAWSInfo("{infraID}-vpc")
	filters := []*ec2.Filter{
		ac.filterByName(vpcName),
		ac.filterByCurrentCluster(),
	}

	result, err := ac.client.DescribeVpcs(&ec2.DescribeVpcsInput{Filters: filters})
	if err != nil {
		return "", err
	}

	if len(result.Vpcs) == 0 {
		return "", newNotFoundError("VPC %s", vpcName)
	}

	return *result.Vpcs[0].VpcId, nil
}
