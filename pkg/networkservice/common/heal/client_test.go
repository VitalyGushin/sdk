// Copyright (c) 2020 Doc.ai and/or its affiliates.
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

package heal_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	"google.golang.org/grpc"

	"github.com/networkservicemesh/sdk/pkg/networkservice/common/heal"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/chain"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/eventchannel"
	"github.com/networkservicemesh/sdk/pkg/tools/addressof"

	"github.com/networkservicemesh/api/pkg/api/networkservice"
	"github.com/stretchr/testify/require"
)

const (
	waitForTimeout = 5 * time.Second
	tickTimeout    = 10 * time.Millisecond
)

type testOnHeal struct {
	RequestFunc func(ctx context.Context, in *networkservice.NetworkServiceRequest, opts ...grpc.CallOption) (*networkservice.Connection, error)
	CloseFunc   func(ctx context.Context, in *networkservice.Connection, opts ...grpc.CallOption) (*empty.Empty, error)
}

func (t *testOnHeal) Request(ctx context.Context, in *networkservice.NetworkServiceRequest, opts ...grpc.CallOption) (*networkservice.Connection, error) {
	return t.RequestFunc(ctx, in, opts...)
}

func (t *testOnHeal) Close(ctx context.Context, in *networkservice.Connection, opts ...grpc.CallOption) (*empty.Empty, error) {
	return t.CloseFunc(ctx, in, opts...)
}

func TestHealClient_Request(t *testing.T) {
	eventCh := make(chan *networkservice.ConnectionEvent, 1)

	onHealCh := make(chan struct{})
	onHeal := &testOnHeal{
		RequestFunc: func(ctx context.Context, in *networkservice.NetworkServiceRequest, opts ...grpc.CallOption) (connection *networkservice.Connection, e error) {
			close(onHealCh)
			return &networkservice.Connection{}, nil
		},
	}

	client := chain.NewNetworkServiceClient(
		heal.NewClient(context.Background(),
			eventchannel.NewMonitorConnectionClient(eventCh), addressof.NetworkServiceClient(onHeal)))

	_, err := client.Request(context.Background(), &networkservice.NetworkServiceRequest{
		Connection: &networkservice.Connection{
			Id:             "conn-1",
			NetworkService: "ns-1",
		},
	})
	require.Nil(t, err)

	eventCh <- &networkservice.ConnectionEvent{
		Type: networkservice.ConnectionEventType_INITIAL_STATE_TRANSFER,
		Connections: map[string]*networkservice.Connection{
			"conn-1": {
				Id:             "conn-1",
				NetworkService: "ns-1",
			},
		},
	}

	eventCh <- &networkservice.ConnectionEvent{
		Type: networkservice.ConnectionEventType_DELETE,
		Connections: map[string]*networkservice.Connection{
			"conn-1": {
				Id:             "conn-1",
				NetworkService: "ns-1",
			},
		},
	}

	cond := func() bool {
		select {
		case <-onHealCh:
			return true
		default:
			return false
		}
	}
	require.Eventually(t, cond, waitForTimeout, tickTimeout)
}

func TestHealClient_MonitorClose(t *testing.T) {
	eventCh := make(chan *networkservice.ConnectionEvent, 1)

	onHealCh := make(chan struct{})
	onHeal := &testOnHeal{
		RequestFunc: func(ctx context.Context, in *networkservice.NetworkServiceRequest, opts ...grpc.CallOption) (connection *networkservice.Connection, e error) {
			close(onHealCh)
			return &networkservice.Connection{}, nil
		},
	}

	ctx, cancelFunc := context.WithCancel(context.Background())
	client := chain.NewNetworkServiceClient(
		heal.NewClient(ctx, eventchannel.NewMonitorConnectionClient(eventCh), addressof.NetworkServiceClient(onHeal)))

	_, err := client.Request(context.Background(), &networkservice.NetworkServiceRequest{
		Connection: &networkservice.Connection{
			Id:             "conn-1",
			NetworkService: "ns-1",
		},
	})
	require.Nil(t, err)

	eventCh <- &networkservice.ConnectionEvent{
		Type: networkservice.ConnectionEventType_INITIAL_STATE_TRANSFER,
		Connections: map[string]*networkservice.Connection{
			"conn-1": {
				Id:             "conn-1",
				NetworkService: "ns-1",
			},
		},
	}

	cancelFunc()

	cond := func() bool {
		select {
		case <-onHealCh:
			return true
		default:
			return false
		}
	}
	require.Eventually(t, cond, waitForTimeout, tickTimeout)
}

func TestHealClient_EmptyInit(t *testing.T) {
	eventCh := make(chan *networkservice.ConnectionEvent, 1)

	client := chain.NewNetworkServiceClient(
		heal.NewClient(context.Background(), eventchannel.NewMonitorConnectionClient(eventCh), nil))

	_, err := client.Request(context.Background(), &networkservice.NetworkServiceRequest{
		Connection: &networkservice.Connection{
			Id:             "conn-1",
			NetworkService: "ns-1",
		},
	})
	require.Nil(t, err)

	eventCh <- &networkservice.ConnectionEvent{
		Type:        networkservice.ConnectionEventType_INITIAL_STATE_TRANSFER,
		Connections: make(map[string]*networkservice.Connection),
	}

	eventCh <- &networkservice.ConnectionEvent{
		Type: networkservice.ConnectionEventType_UPDATE,
		Connections: map[string]*networkservice.Connection{
			"conn-1": {
				Id:             "conn-1",
				NetworkService: "ns-1",
			},
		},
	}
}

func TestNewClient_MissingConnectionsInInit(t *testing.T) {
	eventCh := make(chan *networkservice.ConnectionEvent, 1)

	requestCh := make(chan *networkservice.NetworkServiceRequest)
	onHeal := &testOnHeal{
		RequestFunc: func(ctx context.Context, in *networkservice.NetworkServiceRequest, opts ...grpc.CallOption) (connection *networkservice.Connection, e error) {
			requestCh <- in
			return &networkservice.Connection{}, nil
		},
	}

	ctx, cancelFunc := context.WithCancel(context.Background())
	client := chain.NewNetworkServiceClient(
		heal.NewClient(ctx, eventchannel.NewMonitorConnectionClient(eventCh), addressof.NetworkServiceClient(onHeal)))

	conns := []*networkservice.Connection{
		{Id: "conn-1", NetworkService: "ns-1"},
		{Id: "conn-2", NetworkService: "ns-2"},
	}

	conn, err := client.Request(context.Background(), &networkservice.NetworkServiceRequest{Connection: conns[0]})
	require.Nil(t, err)
	require.True(t, reflect.DeepEqual(conn, conns[0]))

	conn, err = client.Request(context.Background(), &networkservice.NetworkServiceRequest{Connection: conns[1]})
	require.Nil(t, err)
	require.True(t, reflect.DeepEqual(conn, conns[1]))

	eventCh <- &networkservice.ConnectionEvent{
		Type:        networkservice.ConnectionEventType_INITIAL_STATE_TRANSFER,
		Connections: map[string]*networkservice.Connection{conns[0].GetId(): conns[0]},
	}

	// we emulate situation that server managed to handle only the first connection
	// second connection should came in the UPDATE event, but we emulate server's falling down
	cancelFunc()
	// at that point we expect that 'healClient' start healing both 'conn-1' and 'conn-2'

	healedIDs := map[string]bool{}

	cond := func() bool {
		select {
		case r := <-requestCh:
			if _, ok := healedIDs[r.GetConnection().GetId()]; !ok {
				healedIDs[r.GetConnection().GetId()] = true
				return true
			}
			return false
		default:
			return false
		}
	}

	require.Eventually(t, cond, waitForTimeout, tickTimeout)
	require.Eventually(t, cond, waitForTimeout, tickTimeout)

	require.True(t, healedIDs[conns[0].GetId()])
	require.True(t, healedIDs[conns[1].GetId()])
}