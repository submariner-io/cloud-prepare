/*
SPDX-License-Identifier: Apache-2.0

Copyright Contributors to the Submariner project.

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
package azure

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2021-03-01/network"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/pkg/errors"
	"github.com/submariner-io/cloud-prepare/pkg/api"
)

// TODO Make this private once gwdeployer is done

func (c *CloudInfo) CreateSubmarinerLoadBalancingRules(infraID string, ports []api.PortSpec,
	loadBalancerClient *network.LoadBalancersClient) error {
	loadBalancer, err := getLoadBalancer(infraID, loadBalancerClient, c.BaseGroupName)
	if err != nil {
		return errors.Wrapf(err, "getting the loadblancer %q failed", infraID)
	}

	isFound := checkIfInboundNatRulesPresent(&loadBalancer)
	if isFound {
		return nil
	}

	idPrefix := fmt.Sprintf("subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/loadBalancers",
		c.SubscriptionID, c.BaseGroupName)
	frontEndIPConfigurationID := to.StringPtr(fmt.Sprintf("/%s/%s/frontendIPConfigurations/%s", idPrefix,
		infraID, frontendIPConfigurationName))

	inboundNatRules := []network.InboundNatRule{}
	for _, port := range ports {
		inboundNatRules = append(inboundNatRules, c.createInboundNatRule(port.Port, port.Protocol, frontEndIPConfigurationID))
	}

	inboundNatRules = append(inboundNatRules, *loadBalancer.InboundNatRules...)
	loadBalancer.InboundNatRules = &inboundNatRules

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	future, err := loadBalancerClient.CreateOrUpdate(ctx, c.BaseGroupName, infraID, loadBalancer)
	if err != nil {
		return errors.Wrapf(err, "creating loadbalancer %q failed", infraID)
	}

	err = future.WaitForCompletionRef(ctx, loadBalancerClient.Client)

	return errors.Wrapf(err, "Error creating  loadbalancer group %v ", infraID)
}

// TODO Make this private once gwdeployer is done

func (c *CloudInfo) DeleteSubmarinerLoadBalancingRules(infraID string,
	loadBalancerClient *network.LoadBalancersClient) error {
	loadBalancer, err := getLoadBalancer(infraID, loadBalancerClient, c.BaseGroupName)
	if err != nil {
		return errors.Wrapf(err, "getting the loadblancer %q failed", infraID)
	}

	var inboundNatRules []network.InboundNatRule

	for _, existingInboundNatRule := range *loadBalancer.InboundNatRules {
		if existingInboundNatRule.Name != nil && !strings.Contains(*existingInboundNatRule.Name, inboundRulePrefix) {
			inboundNatRules = append(inboundNatRules, existingInboundNatRule)
		}
	}

	loadBalancer.InboundNatRules = &inboundNatRules

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	future, err := loadBalancerClient.CreateOrUpdate(ctx, c.BaseGroupName, infraID, loadBalancer)
	if err != nil {
		return errors.Wrapf(err, "creating loadbalancer %q failed", infraID)
	}

	err = future.WaitForCompletionRef(ctx, loadBalancerClient.Client)

	return errors.Wrapf(err, "Error creating  loadbalancer group %v ", infraID)
}

func getLoadBalancer(loadBalancerName string, loadBalancerClient *network.LoadBalancersClient,
	baseGroupName string) (network.LoadBalancer, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	lb, err := loadBalancerClient.Get(ctx, baseGroupName, loadBalancerName, "")

	return lb, errors.Wrapf(err, "error getting the loadbalancer %q", loadBalancerName)
}

func (c *CloudInfo) createInboundNatRule(port uint16, protocol string, frontendIPConfigurationID *string) network.InboundNatRule {
	return network.InboundNatRule{
		InboundNatRulePropertiesFormat: &network.InboundNatRulePropertiesFormat{
			Protocol:             network.TransportProtocol(protocol),
			FrontendPort:         to.Int32Ptr(int32(port)),
			BackendPort:          to.Int32Ptr(int32(port)),
			IdleTimeoutInMinutes: to.Int32Ptr(4),
			FrontendIPConfiguration: &network.SubResource{
				ID: frontendIPConfigurationID,
			},
		},
		Name: to.StringPtr(inboundRulePrefix + protocol + "-" + strconv.Itoa(int(port))),
	}
}

func checkIfInboundNatRulesPresent(loadBalancer *network.LoadBalancer) bool {
	for _, existingInboundNatRule := range *loadBalancer.InboundNatRules {
		if existingInboundNatRule.Name != nil && strings.Contains(*existingInboundNatRule.Name, inboundRulePrefix) {
			return true
		}
	}

	return false
}
