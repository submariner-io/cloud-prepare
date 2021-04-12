package aws

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
)

const permissionsTest = "permissions-test"

func determinePermissionError(err error, operation string) error {
	if err == nil || isAWSError(err, "DryRunOperation") {
		return nil
	} else if isAWSError(err, "UnauthorizedOperation") {
		return fmt.Errorf("No permissions to %s", operation)
	}
	return fmt.Errorf("Error while checking permissions for %s, details: %s", operation, err)
}

func dryRunOK(err error) bool {
	return isAWSError(err, "DryRunOperation")
}

func (ac *awsCloud) validateCreateSecGroup(vpcID string) error {
	input := &ec2.CreateSecurityGroupInput{
		DryRun:      aws.Bool(true),
		GroupName:   aws.String(permissionsTest),
		Description: aws.String(permissionsTest),
		VpcId:       aws.String(vpcID),
	}

	_, err := ac.client.CreateSecurityGroup(input)
	return determinePermissionError(err, "create security group")
}

func (ac *awsCloud) validateCreateSecGroupRule(vpcID string) error {
	workerGroupID, err := ac.getSecurityGroupID(vpcID, "{infraID}-worker-sg")
	if err != nil {
		return err
	}

	input := &ec2.AuthorizeSecurityGroupIngressInput{
		DryRun:  aws.Bool(true),
		GroupId: workerGroupID,
	}

	_, err = ac.client.AuthorizeSecurityGroupIngress(input)
	return determinePermissionError(err, "authorize security group ingress")
}

func (ac *awsCloud) validateCreateTag(subnetID *string) error {
	_, err := ac.client.CreateTags(&ec2.CreateTagsInput{
		DryRun:    aws.Bool(true),
		Resources: []*string{subnetID},
		Tags: []*ec2.Tag{
			tagSubmarinerGatgeway,
		},
	})
	return determinePermissionError(err, "create tags on subnets")
}

func (ac *awsCloud) validateDeleteSecGroup(vpcID string) error {
	workerGroupID, err := ac.getSecurityGroupID(vpcID, "{infraID}-worker-sg")
	if err != nil {
		return err
	}

	input := &ec2.DeleteSecurityGroupInput{
		DryRun:  aws.Bool(true),
		GroupId: workerGroupID,
	}

	_, err = ac.client.DeleteSecurityGroup(input)
	return determinePermissionError(err, "delete security group")
}

func (ac *awsCloud) validateDeleteSecGroupRule(vpcID string) error {
	workerGroupID, err := ac.getSecurityGroupID(vpcID, "{infraID}-worker-sg")
	if err != nil {
		return err
	}
	input := &ec2.RevokeSecurityGroupIngressInput{
		DryRun:  aws.Bool(true),
		GroupId: workerGroupID,
	}

	_, err = ac.client.RevokeSecurityGroupIngress(input)
	return determinePermissionError(err, "revoke security group ingress")
}

func (ac *awsCloud) validateRemoveTag(subnetID *string) error {
	_, err := ac.client.DeleteTags(&ec2.DeleteTagsInput{
		DryRun:    aws.Bool(true),
		Resources: []*string{subnetID},
		Tags: []*ec2.Tag{
			tagSubmarinerGatgeway,
		},
	})
	return determinePermissionError(err, "delete tags from subnets")
}
