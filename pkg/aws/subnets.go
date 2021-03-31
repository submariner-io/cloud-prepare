package aws

import "github.com/aws/aws-sdk-go/service/ec2"

const (
	tagSubmarinerGatgeway = "submariner.io/gateway"
	tagInternalELB        = "kubernetes.io/role/internal-elb"
)

func (ac *awsCloud) getPublicSubnet(vpcID string) (*ec2.Subnet, error) {
	filters := []*ec2.Filter{
		ec2Filter("vpc-id", vpcID),
		ac.filterByName("{infraID}-public-{region}*"),
		ac.filterByCurrentCluster(),
	}

	result, err := ac.client.DescribeSubnets(&ec2.DescribeSubnetsInput{Filters: filters})
	if err != nil {
		return nil, err
	}

	if len(result.Subnets) == 0 {
		return nil, newNotFoundError("public subnet")
	}

	return result.Subnets[0], nil
}

func (ac *awsCloud) tagPublicSubnet(subnetID *string) error {
	_, err := ac.client.CreateTags(&ec2.CreateTagsInput{
		Resources: []*string{subnetID},
		Tags: []*ec2.Tag{
			ec2Tag(tagInternalELB, ""),
			ec2Tag(tagSubmarinerGatgeway, ""),
		},
	})

	return err
}
