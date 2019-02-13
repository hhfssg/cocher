package discovery

import (
	"context"
	"strings"

	"github.com/cocher/utils/log"
	"github.com/cocher/dht"
	"github.com/cocher/internal/protobuf"
	"github.com/cocher/network"
	"github.com/cocher/peer"
)

type Component struct {
	*network.Component

	DisablePing   bool
	DisablePong   bool
	DisableLookup bool

	Routes *dht.RoutingTable
}

var (
	ComponentID                            = (*Component)(nil)
	_           network.ComponentInterface = (*Component)(nil)
)

func (state *Component) Startup(net *network.Network) {
	// Create routing table.
	state.Routes = dht.CreateRoutingTable(net.ID)
}

func (state *Component) Receive(ctx *network.ComponentContext) error {
	// Update routing for every incoming message.
	state.Routes.Update(ctx.Sender())
	gCtx := network.WithSignMessage(context.Background(), true)

	// Handle RPC.
	switch msg := ctx.Message().(type) {
	case *protobuf.Ping:
		if state.DisablePing {
			break
		}

		// Send pong to peer.
		err := ctx.Reply(gCtx, &protobuf.Pong{})

		if err != nil {
			return err
		}
	case *protobuf.Pong:
		if state.DisablePong {
			break
		}

		//todo delete for walking around
		//peers := FindNode(ctx.Network(), ctx.Sender(), dht.BucketSize, 8)

		// Update routing table w/ closest peers to self.
		/*
			for _, peerID := range peers {
				state.Routes.Update(peerID)
			}
		*/

		log.Infof("bootstrapped w/ peer(s): %s.", strings.Join(state.Routes.GetPeerAddresses(), ", "))
	case *protobuf.LookupNodeRequest:
		if state.DisableLookup {
			break
		}

		// Prepare response.
		response := &protobuf.LookupNodeResponse{}

		// Respond back with closest peers to a provided target.
		for _, peerID := range state.Routes.FindClosestPeers(peer.ID(*msg.Target), dht.BucketSize) {
			id := protobuf.ID(peerID)
			response.Peers = append(response.Peers, &id)
		}

		err := ctx.Reply(gCtx, response)
		if err != nil {
			return err
		}

		log.Infof("connected peers: %s.", strings.Join(state.Routes.GetPeerAddresses(), ", "))
	case *protobuf.Disconnect:
		ctx.Disconnect()
	}

	return nil
}

func (state *Component) Cleanup(net *network.Network) {
	// TODO: Save routing table?
}

func (state *Component) PeerDisconnect(client *network.PeerClient) {
	// Delete peer if in routing table.
	if client.ID != nil {
		if state.Routes.PeerExists(*client.ID) {
			state.Routes.RemovePeer(*client.ID)

			log.Infof("Peer %s has disconnected from %s.", client.ID.Address, client.Network.ID.Address)
		}
	}
}
