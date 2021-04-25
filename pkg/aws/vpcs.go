/*
© 2021 Red Hat, Inc. and others.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
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
