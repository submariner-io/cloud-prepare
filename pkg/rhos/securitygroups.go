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

package rhos

import (
	"context"
	"net/http"
	"slices"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/secgroups"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/rules"
	"github.com/gophercloud/gophercloud/v2/pagination"
	"github.com/pkg/errors"
	"github.com/submariner-io/cloud-prepare/pkg/api"
	"github.com/submariner-io/cloud-prepare/pkg/k8s"
)

var (
	ListSecurityGroups    = secgroups.List
	ExtractSecurityGroups = secgroups.ExtractSecurityGroups
	CreateSecurityGroup   = func(ctx context.Context, c *gophercloud.ServiceClient, opts secgroups.CreateOptsBuilder) (
		*secgroups.SecurityGroup, error,
	) {
		return secgroups.Create(ctx, c, opts).Extract()
	}
	DeleteSecurityGroup = secgroups.Delete
	AddServer           = func(ctx context.Context, c *gophercloud.ServiceClient, serverID string, groupName string) error {
		return secgroups.AddServer(ctx, c, serverID, groupName).ExtractErr()
	}
	RemoveServer = func(ctx context.Context, c *gophercloud.ServiceClient, serverID string, groupName string) error {
		return secgroups.RemoveServer(ctx, c, serverID, groupName).ExtractErr()
	}
	EachPage = func(ctx context.Context, pager pagination.Pager, handler func(context.Context, pagination.Page) (bool, error)) error {
		return pager.EachPage(ctx, handler)
	}
	CreateRule = func(ctx context.Context, c *gophercloud.ServiceClient, opts rules.CreateOptsBuilder) (*rules.SecGroupRule, error) {
		return rules.Create(ctx, c, opts).Extract()
	}
	ListServers    = servers.List
	ExtractServers = servers.ExtractServers
)

type CloudInfo struct {
	Client      *gophercloud.ProviderClient
	InfraID     string
	Region      string
	SubnetNames []string
	K8sClient   k8s.Interface
}

func (c *CloudInfo) openInternalPorts(ctx context.Context, infraID string, ports []api.PortSpec,
	computeClient, networkClient *gophercloud.ServiceClient,
) error {
	var group *secgroups.SecurityGroup
	groupName := infraID + InternalSecurityGroupSuffix
	opts := secgroups.CreateOpts{
		Name:        groupName,
		Description: "Submariner Internal",
	}

	isFound, err := checkIfSecurityGroupPresent(ctx, groupName, computeClient)
	if err != nil {
		return err
	}

	if !isFound {
		group, err = CreateSecurityGroup(ctx, computeClient, opts)
		if err != nil {
			return errors.WithMessagef(err, "creating security group failed")
		}

		for _, port := range ports {
			err = c.createSGRule(ctx, group.ID, group.ID, "", port.Port, port.Protocol, networkClient)
			if err != nil {
				return errors.WithMessage(err, "creating security group rule failed")
			}
		}
	}

	return addServerSecurityGroups(ctx, c.InfraID, groupName, computeClient)
}

func addServerSecurityGroups(ctx context.Context, serverName, groupName string, computeClient *gophercloud.ServiceClient) error {
	pager := ListServers(computeClient, servers.ListOpts{Name: serverName})
	err := EachPage(ctx, pager, func(ctx context.Context, page pagination.Page) (bool, error) {
		serverList, err := ExtractServers(page)
		if err != nil {
			return false, errors.WithMessage(err, "getting the server List failed")
		}

		for i := range serverList {
			found := false

			for j := range serverList[i].SecurityGroups {
				existingGroupName, ok := serverList[i].SecurityGroups[j]["name"]
				if ok && existingGroupName == groupName {
					found = true
				}
			}

			if !found {
				err = AddServer(ctx, computeClient, serverList[i].ID, groupName)
				if err != nil {
					return false, errors.WithMessagef(err, "failed to add security group %q to server %s", groupName, serverList[i].ID)
				}
			}
		}

		return true, nil
	})

	return errors.WithMessage(err, "failed to add security group to servers")
}

func (c *CloudInfo) removeInternalFirewallRules(ctx context.Context, infraID string, computeClient *gophercloud.ServiceClient) error {
	return removeServerSecurityGroups(ctx, c.InfraID, infraID+InternalSecurityGroupSuffix, computeClient)
}

func removeServerSecurityGroups(ctx context.Context, serverName, groupName string, computeClient *gophercloud.ServiceClient) error {
	pager := ListServers(computeClient, servers.ListOpts{Name: serverName})

	err := EachPage(ctx, pager, func(ctx context.Context, page pagination.Page) (bool, error) {
		serverList, err := ExtractServers(page)
		if err != nil {
			return false, errors.WithMessage(err, "getting the server List failed")
		}

		for i := range serverList {
			err = RemoveServer(ctx, computeClient, serverList[i].ID, groupName)
			if err != nil {
				if gophercloud.ResponseCodeIs(err, http.StatusNotFound) {
					continue
				}

				return false, errors.WithMessagef(err, "failed to remove the firewall for the server: %q ", serverList[i].Name)
			}
		}

		return true, nil
	})

	return errors.WithMessage(err, "failed to remove security group from servers")
}

func (c *CloudInfo) createGWSecurityGroup(ctx context.Context, ports []api.PortSpec, groupName string,
	computeClient *gophercloud.ServiceClient, networkClient *gophercloud.ServiceClient,
) error {
	isFound, err := checkIfSecurityGroupPresent(ctx, groupName, computeClient)
	if err != nil {
		return err
	}

	if isFound {
		return nil
	}

	opts := secgroups.CreateOpts{
		Name:        groupName,
		Description: "Submariner Gateway",
	}

	group, err := CreateSecurityGroup(ctx, computeClient, opts)
	if err != nil {
		return errors.WithMessage(err, "failed to create g/w security group")
	}

	for _, port := range ports {
		err = c.createSGRule(ctx, group.ID, "", allNetworkCIDR, port.Port, port.Protocol, networkClient)
		if err != nil {
			return errors.WithMessagef(err, "creating security group rule failed")
		}
	}

	return nil
}

func checkIfSecurityGroupPresent(ctx context.Context, groupName string, computeClient *gophercloud.ServiceClient) (bool, error) {
	pager := ListSecurityGroups(computeClient)
	var isFound bool

	err := EachPage(ctx, pager, func(ctx context.Context, page pagination.Page) (bool, error) {
		serverList, err := ExtractSecurityGroups(page)

		isFound = slices.ContainsFunc(serverList, func(s secgroups.SecurityGroup) bool {
			return s.Name == groupName
		})

		return !isFound, errors.WithMessagef(err, "failed to extract the security group %q from results", groupName)
	})

	return isFound, errors.WithMessagef(err, "error getting the security group : %q", groupName)
}

func (c *CloudInfo) deleteSG(ctx context.Context, groupName string, computeClient *gophercloud.ServiceClient) error {
	pager := ListSecurityGroups(computeClient)
	var isFound bool
	var securityGroupID string

	err := EachPage(ctx, pager, func(ctx context.Context, page pagination.Page) (bool, error) {
		serverList, err := ExtractSecurityGroups(page)
		if err != nil {
			return false, errors.WithMessagef(err, "failed to list the security group %q", groupName)
		}

		for _, s := range serverList {
			if s.Name == groupName {
				isFound = true
				securityGroupID = s.ID

				break
			}
		}

		return !isFound, nil
	})

	if err == nil && isFound {
		err = DeleteSecurityGroup(ctx, computeClient, securityGroupID).ExtractErr()
	}

	return errors.WithMessagef(err, "error deleting the security group %q", groupName)
}

func (c *CloudInfo) createSGRule(ctx context.Context, group, remoteGroupID, remoteIPPrefix string, port uint16,
	protocol string, networkClient *gophercloud.ServiceClient,
) error {
	opts := rules.CreateOpts{
		Direction:      "ingress",
		EtherType:      rules.EtherType4,
		SecGroupID:     group,
		PortRangeMax:   int(port),
		PortRangeMin:   int(port),
		Protocol:       rules.RuleProtocol(protocol),
		RemoteGroupID:  remoteGroupID,
		RemoteIPPrefix: remoteIPPrefix,
	}

	_, err := CreateRule(ctx, networkClient, opts)

	return errors.WithMessagef(err, "failed creating security group rule with port %d , protocol %q,"+
		"remotegroupID %q, remoteIPprefix %q , in security group %q", port, protocol, remoteGroupID, remoteIPPrefix, group)
}
