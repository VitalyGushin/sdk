// Copyright (c) 2022 Cisco and/or its affiliates.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package authorize provides authorization checks for incoming or returning requests.
package authorize

import (
	"context"
	"sync/atomic"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"

	"github.com/networkservicemesh/api/pkg/api/registry"

	"github.com/networkservicemesh/sdk/pkg/registry/common/grpcmetadata"
	"github.com/networkservicemesh/sdk/pkg/registry/core/next"
	"github.com/networkservicemesh/sdk/pkg/tools/opa"
	"github.com/networkservicemesh/sdk/pkg/tools/postpone"
)

type authorizeNSEClient struct {
	policies      policiesList
	nsePathIdsMap *PathIdsMap
	serverPeer    atomic.Value
}

// NewNetworkServiceEndpointRegistryClient - returns a new authorization registry.NetworkServiceEndpointRegistryClient
// Authorize registry client checks path of NSE.
func NewNetworkServiceEndpointRegistryClient(opts ...Option) registry.NetworkServiceEndpointRegistryClient {
	o := &options{
		resourcePathIdsMap: new(PathIdsMap),
	}

	for _, opt := range opts {
		opt(o)
	}

	policies, err := opa.PoliciesByFileMask(o.policyPaths...)
	if err != nil {
		panic(errors.Wrap(err, "failed to read policies in NetworkServiceRegistry authorize client").Error())
	}

	var policyList policiesList
	for _, p := range policies {
		policyList = append(policyList, p)
	}

	return &authorizeNSEClient{
		policies:      policyList,
		nsePathIdsMap: o.resourcePathIdsMap,
	}
}

func (c *authorizeNSEClient) Register(ctx context.Context, nse *registry.NetworkServiceEndpoint, opts ...grpc.CallOption) (*registry.NetworkServiceEndpoint, error) {
	if len(c.policies) == 0 {
		return next.NetworkServiceEndpointRegistryClient(ctx).Register(ctx, nse, opts...)
	}

	path := grpcmetadata.PathFromContext(ctx)

	ctx = grpcmetadata.PathWithContext(ctx, path)

	var p peer.Peer
	opts = append(opts, grpc.Peer(&p))

	postponeCtxFunc := postpone.ContextWithValues(ctx)

	resp, err := next.NetworkServiceEndpointRegistryClient(ctx).Register(ctx, nse, opts...)
	if err != nil {
		return nil, err
	}

	if p != (peer.Peer{}) {
		c.serverPeer.Store(&p)
		ctx = peer.NewContext(ctx, &p)
	}

	path = grpcmetadata.PathFromContext(ctx)

	spiffeID, err := getSpiffeIDFromPath(path)
	if err != nil {
		return nil, err
	}

	rawMap := getRawMap(c.nsePathIdsMap)
	input := RegistryOpaInput{
		ResourceID:         spiffeID.String(),
		ResourceName:       resp.Name,
		ResourcePathIdsMap: rawMap,
		PathSegments:       path.PathSegments,
		Index:              path.Index,
	}
	if err := c.policies.check(ctx, input); err != nil {
		unregisterCtx, cancelUnregister := postponeCtxFunc()
		defer cancelUnregister()

		if _, unregisterErr := next.NetworkServiceEndpointRegistryClient(ctx).Unregister(unregisterCtx, resp, opts...); unregisterErr != nil {
			err = errors.Wrapf(err, "nse unregistered with error: %s", unregisterErr.Error())
		}

		return nil, err
	}

	c.nsePathIdsMap.Store(resp.Name, resp.PathIds)
	return resp, nil
}

func (c *authorizeNSEClient) Find(ctx context.Context, query *registry.NetworkServiceEndpointQuery, opts ...grpc.CallOption) (registry.NetworkServiceEndpointRegistry_FindClient, error) {
	return next.NetworkServiceEndpointRegistryClient(ctx).Find(ctx, query, opts...)
}

func (c *authorizeNSEClient) Unregister(ctx context.Context, nse *registry.NetworkServiceEndpoint, opts ...grpc.CallOption) (*empty.Empty, error) {
	if len(c.policies) == 0 {
		return next.NetworkServiceEndpointRegistryClient(ctx).Unregister(ctx, nse, opts...)
	}

	path := grpcmetadata.PathFromContext(ctx)

	ctx = grpcmetadata.PathWithContext(ctx, path)

	resp, err := next.NetworkServiceEndpointRegistryClient(ctx).Unregister(ctx, nse, opts...)
	if err != nil {
		return nil, err
	}

	p, ok := c.serverPeer.Load().(*peer.Peer)
	if ok && p != nil {
		ctx = peer.NewContext(ctx, p)
	}

	path = grpcmetadata.PathFromContext(ctx)

	spiffeID, err := getSpiffeIDFromPath(path)
	if err != nil {
		return nil, err
	}

	rawMap := getRawMap(c.nsePathIdsMap)
	input := RegistryOpaInput{
		ResourceID:         spiffeID.String(),
		ResourceName:       nse.Name,
		ResourcePathIdsMap: rawMap,
		PathSegments:       path.PathSegments,
		Index:              path.Index,
	}

	if err := c.policies.check(ctx, input); err != nil {
		return nil, err
	}

	c.nsePathIdsMap.Delete(nse.Name)
	return resp, nil
}
