package aws

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
)

func ec2Filter(name string, value string) *ec2.Filter {
	return &ec2.Filter{
		Name:   aws.String(name),
		Values: []*string{aws.String(value)},
	}
}

func ec2Tag(key string, value string) *ec2.Tag {
	return &ec2.Tag{
		Key:   aws.String(key),
		Value: aws.String(value),
	}
}

func ec2FilterByTag(tag *ec2.Tag) *ec2.Filter {
	return ec2Filter(fmt.Sprintf("tag:%s", *tag.Key), *tag.Value)
}

func extractName(tags []*ec2.Tag) string {
	for _, tag := range tags {
		if *tag.Key == "Name" {
			return *tag.Value
		}
	}

	return ""
}

func (ac *awsCloud) withAWSInfo(str string) string {
	r := strings.NewReplacer("{infraID}", ac.infraID, "{region}", ac.region)
	return r.Replace(str)
}

func (ac *awsCloud) filterByName(name string) *ec2.Filter {
	return ec2Filter("tag:Name", ac.withAWSInfo(name))
}

func (ac *awsCloud) filterByCurrentCluster() *ec2.Filter {
	return ec2Filter(ac.withAWSInfo("tag:kubernetes.io/cluster/{infraID}"), "owned")
}
