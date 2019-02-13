package network

import (
	"context"

	"github.com/gogo/protobuf/proto"
	"github.com/cocher/peer"
)

// ComponentContext provides parameters and helper functions to a Component
// for interacting with/analyzing incoming messages from a select peer.
type ComponentContext struct {
	client  *PeerClient
	message proto.Message
	nonce   uint64
}

// Reply sends back a message to an incoming message's incoming stream.
func (pctx *ComponentContext) Reply(ctx context.Context, message proto.Message) error {
	return pctx.client.Reply(ctx, pctx.nonce, message)
}

// Message returns the decoded protobuf message.
func (pctx *ComponentContext) Message() proto.Message {
	return pctx.message
}

// Client returns the peer client.
func (pctx *ComponentContext) Client() *PeerClient {
	return pctx.client
}

// Network returns the entire node's network.
func (pctx *ComponentContext) Network() *Network {
	return pctx.client.Network
}

// Self returns the node's ID.
func (pctx *ComponentContext) Self() peer.ID {
	return pctx.Network().ID
}

// Sender returns the peer's ID.
func (pctx *ComponentContext) Sender() peer.ID {
	return *pctx.client.ID
}

func (pctx *ComponentContext) Disconnect() {
	pctx.client.Close()
}
