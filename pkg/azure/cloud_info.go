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
	"strconv"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2021-03-01/network"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/pkg/errors"
	"github.com/submariner-io/cloud-prepare/pkg/api"
	"github.com/submariner-io/cloud-prepare/pkg/k8s"
)

const (
	internalSecurityGroupSuffix = "-nsg"
	externalSecurityGroupSuffix = "-submariner-external-sg"
	internalSecurityRulePrefix  = "Submariner-Internal-"
	externalSecurityRulePrefix  = "Submariner-External-"
	allNetworkCIDR              = "0.0.0.0/0"
	basePriorityInternal        = 2500
	baseExternalInternal        = 3500
)

type CloudInfo struct {
	SubscriptionID string
	InfraID        string
	Region         string
	BaseGroupName  string
	Authorizer     autorest.Authorizer
	K8sClient      k8s.Interface
}

func (c *CloudInfo) openInternalPorts(infraID string, ports []api.PortSpec, sgClient *network.SecurityGroupsClient) error {
	groupName := infraID + internalSecurityGroupSuffix

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	nwSecurityGroup, err := sgClient.Get(ctx, c.BaseGroupName, groupName, "")
	if err != nil {
		return errors.Wrapf(err, "error getting the security group %q", groupName)
	}

	isFound := checkIfSecurityRulesPresent(nwSecurityGroup)
	if isFound {
		return nil
	}

	securityRules := *nwSecurityGroup.SecurityRules
	for i, port := range ports {
		securityRules = append(securityRules, c.createSecurityRule(internalSecurityRulePrefix,
			port.Protocol, port.Port, int32(basePriorityInternal+i), network.SecurityRuleDirectionInbound),
			c.createSecurityRule(internalSecurityRulePrefix, port.Protocol, port.Port,
				int32(basePriorityInternal+i), network.SecurityRuleDirectionOutbound))
	}

	nwSecurityGroup.SecurityRules = &securityRules

	future, err := sgClient.CreateOrUpdate(ctx, c.BaseGroupName, groupName, nwSecurityGroup)
	if err != nil {
		return errors.Wrapf(err, "updating security group %q with submariner rules failed", groupName)
	}

	err = future.WaitForCompletionRef(ctx, sgClient.Client)

	return errors.Wrapf(err, "error updating  security group %q with submariner rules", groupName)
}

func (c *CloudInfo) removeInternalFirewallRules(infraID string, sgClient *network.SecurityGroupsClient) error {
	groupName := infraID + internalSecurityGroupSuffix

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	nwSecurityGroup, err := sgClient.Get(ctx, c.BaseGroupName, groupName, "")
	if err != nil {
		return errors.Wrapf(err, "error getting the security group %q", groupName)
	}

	securityRules := []network.SecurityRule{}

	for _, existingSGRule := range *nwSecurityGroup.SecurityRules {
		if existingSGRule.Name != nil && !strings.Contains(*existingSGRule.Name, internalSecurityRulePrefix) {
			securityRules = append(securityRules, existingSGRule)
		}
	}

	nwSecurityGroup.SecurityRules = &securityRules

	future, err := sgClient.CreateOrUpdate(ctx, c.BaseGroupName, groupName, nwSecurityGroup)
	if err != nil {
		return errors.Wrapf(err, "removing submariner rules from  security group %q failed", groupName)
	}

	err = future.WaitForCompletionRef(ctx, sgClient.Client)

	return errors.Wrapf(err, "removing submariner rules from security group %q failed", groupName)
}

func checkIfSecurityRulesPresent(securityGroup network.SecurityGroup) bool {
	for _, existingSGRule := range *securityGroup.SecurityRules {
		if existingSGRule.Name != nil && strings.Contains(*existingSGRule.Name, internalSecurityRulePrefix) {
			return true
		}
	}

	return false
}

func (c *CloudInfo) createSecurityRule(securityRulePrfix, protocol string, port uint16, priority int32,
	ruleDirection network.SecurityRuleDirection,
) network.SecurityRule {
	return network.SecurityRule{
		Name: to.StringPtr(securityRulePrfix + protocol + "-" + strconv.Itoa(int(port)) + "-" + string(ruleDirection)),
		SecurityRulePropertiesFormat: &network.SecurityRulePropertiesFormat{
			Protocol:                 network.SecurityRuleProtocol(protocol),
			DestinationPortRange:     to.StringPtr(strconv.Itoa(int(port)) + "-" + strconv.Itoa(int(port))),
			SourceAddressPrefix:      to.StringPtr(allNetworkCIDR),
			DestinationAddressPrefix: to.StringPtr(allNetworkCIDR),
			SourcePortRange:          to.StringPtr("*"),
			Access:                   network.SecurityRuleAccessAllow,
			Direction:                ruleDirection,
			Priority:                 to.Int32Ptr(priority),
		},
	}
}

func (c *CloudInfo) createGWSecurityGroup(infraID string, ports []api.PortSpec, sgClient *network.SecurityGroupsClient) error {
	groupName := infraID + externalSecurityGroupSuffix

	isFound := checkIfSecurityGroupPresent(groupName, sgClient, c.BaseGroupName)
	if isFound {
		return nil
	}

	securityRules := []network.SecurityRule{}
	for i, port := range ports {
		securityRules = append(securityRules, c.createSecurityRule(externalSecurityRulePrefix, port.Protocol,
			port.Port, int32(baseExternalInternal+i), network.SecurityRuleDirectionInbound),
			c.createSecurityRule(externalSecurityRulePrefix, port.Protocol, port.Port,
				int32(baseExternalInternal+i), network.SecurityRuleDirectionOutbound))
	}

	nwSecurityGroup := network.SecurityGroup{
		Name:     &groupName,
		Location: to.StringPtr(c.Region),
		SecurityGroupPropertiesFormat: &network.SecurityGroupPropertiesFormat{
			SecurityRules: &securityRules,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	future, err := sgClient.CreateOrUpdate(ctx, c.BaseGroupName, groupName, nwSecurityGroup)
	if err != nil {
		return errors.Wrapf(err, "creating security group %q failed", groupName)
	}

	err = future.WaitForCompletionRef(ctx, sgClient.Client)

	return errors.Wrapf(err, "Error creating  security group %v ", groupName)
}

// TODO Make this private once gwdeployer is done

func (c *CloudInfo) removeGWSecurityGroup(infraID string, sgClient *network.SecurityGroupsClient,
	nwClient *network.InterfacesClient,
) error {
	groupName := infraID + externalSecurityGroupSuffix
	isFound := checkIfSecurityGroupPresent(groupName, sgClient, c.BaseGroupName)

	if !isFound {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	nwSecurityGroup, err := sgClient.Get(ctx, c.BaseGroupName, groupName, "")
	if err != nil {
		return errors.Wrapf(err, "error getting the submariner gateway security group %q", groupName)
	}

	interfacesInRG, err := nwClient.List(ctx, c.BaseGroupName)
	if err != nil {
		return errors.Wrapf(err, "error getting the interfaces list in resource group %q", c.BaseGroupName)
	}

	if nwSecurityGroup.SecurityGroupPropertiesFormat != nil && nwSecurityGroup.SecurityGroupPropertiesFormat.NetworkInterfaces != nil {
		for _, interfaceWithID := range *nwSecurityGroup.SecurityGroupPropertiesFormat.NetworkInterfaces {
			interfacesInRGValues := interfacesInRG.Values()

			var interfaceWithSG network.Interface

			for _, values := range interfacesInRGValues {
				if *values.ID == *interfaceWithID.ID {
					interfaceWithSG = values
					break
				}
			}

			interfaceWithSG.InterfacePropertiesFormat.NetworkSecurityGroup = nil

			future, err := nwClient.CreateOrUpdate(ctx, c.BaseGroupName, *interfaceWithSG.Name, interfaceWithSG)
			if err != nil {
				return errors.Wrapf(err, "removing  security group %q from interface %q failed", groupName,
					*interfaceWithSG.ID)
			}

			err = future.WaitForCompletionRef(ctx, sgClient.Client)
			if err != nil {
				return errors.Wrapf(err, "updating  interface  %q failed", *interfaceWithSG.Name)
			}
		}
	}

	if err != nil {
		return errors.Wrapf(err, "waiting for the submariner gateway security group  %q to be updated failed", groupName)
	}

	deleteFuture, err := sgClient.Delete(ctx, c.BaseGroupName, groupName)
	if err != nil {
		return errors.Wrapf(err, "deleting security group %q failed", groupName)
	}

	err = deleteFuture.WaitForCompletionRef(ctx, sgClient.Client)

	if err != nil {
		return errors.Wrapf(err, "waiting for the submariner gateway  ecurity group  %q to be deleted failed", groupName)
	}

	return errors.WithMessage(err, "failed to remove the submariner gateway security group from servers")
}

func checkIfSecurityGroupPresent(groupName string, networkClient *network.SecurityGroupsClient, baseGroupName string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	_, err := networkClient.Get(ctx, baseGroupName, groupName, "")

	return err == nil
}
