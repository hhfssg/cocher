package network_test

import (
	"context"
	"testing"
	"time"

	"github.com/cocher/crypto"
	"github.com/cocher/crypto/blake2b"
	"github.com/cocher/crypto/ed25519"
	"github.com/cocher/internal/test/protobuf"
	"github.com/cocher/network"
	"github.com/cocher/network/discovery"
	"github.com/cocher/peer"
	"github.com/cocher/types/opcode"
)

func init() {
	opcode.RegisterMessageType(opcode.Opcode(1000), &protobuf.TestMessage{})
}

type env struct {
	name        string
	networkType string
	hash        crypto.HashPolicy
	signature   crypto.SignaturePolicy
}

var (
	kcpEnv             = env{name: "kcp-blake2b-ed25519", networkType: "kcp", hash: blake2b.New(), signature: ed25519.New()}
	tcpEnv             = env{name: "tcp-blake2b-ed25519", networkType: "tcp", hash: blake2b.New(), signature: ed25519.New()}
	allEnvs            = []env{kcpEnv, tcpEnv}
	mailboxComponentID = (*MailBoxComponent)(nil)
)

type testSuite struct {
	t *testing.T
	e env

	builderOptions []network.BuilderOption
	bootstrapNode  *network.Network
	nodes          []*network.Network
	Components     []*network.Component
}

func newTest(t *testing.T, e env, opts ...network.BuilderOption) *testSuite {
	te := &testSuite{
		t:              t,
		e:              e,
		builderOptions: opts,
	}
	return te
}

func (te *testSuite) startBoostrap(numNodes int, Components ...network.ComponentInterface) {
	for i := 0; i < numNodes; i++ {
		builder := network.NewBuilderWithOptions(te.builderOptions...)
		builder.SetKeys(te.e.signature.RandomKeyPair())
		builder.SetAddress(network.FormatAddress(te.e.networkType, "localhost", uint16(network.GetRandomUnusedPort())))

		builder.AddComponent(new(discovery.Component))
		builder.AddComponent(new(MailBoxComponent))

		for _, Component := range Components {
			builder.AddComponent(Component)
		}

		node, err := builder.Build()
		if err != nil {
			te.t.Fatalf("Build() = expected no error, got %v", err)
		}

		go node.Listen()

		if i == 0 {
			te.bootstrapNode = node
			node.BlockUntilListening()
		} else {
			te.nodes = append(te.nodes, node)
		}
	}

	for _, node := range te.nodes {
		node.Bootstrap(te.bootstrapNode.Address)
	}

	// wait for nodes to discover other peers
	for _, node := range te.nodes {
		ComponentInt, ok := node.Component(discovery.ComponentID)
		if !ok {
			te.t.Fatalf("Component() expected true, got false")
		}

		Component := ComponentInt.(*discovery.Component)
		routes := Component.Routes
		peers := routes.GetPeers()

		for len(peers) < numNodes-1 {
			peers = routes.GetPeers()
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func (te *testSuite) tearDown() {
	for _, node := range te.nodes {
		node.Close()
	}
	te.bootstrapNode.Close()
}

func (te *testSuite) getMailbox(n *network.Network) *MailBoxComponent {
	if n == nil {
		return nil
	}
	ComponentInt, ok := n.Component(mailboxComponentID)
	if !ok {
		te.t.Errorf("Component(mailboxComponentID) expected true, got false")
	}
	return ComponentInt.(*MailBoxComponent)
}

func (te *testSuite) getPeers(n *network.Network) []peer.ID {
	if n == nil {
		return nil
	}
	ComponentInt, ok := n.Component(discovery.ComponentID)
	if !ok {
		te.t.Errorf("Component() expected true, got false")
	}
	Component := ComponentInt.(*discovery.Component)
	routes := Component.Routes
	return routes.GetPeers()
}

// MailBoxComponent buffers all messages into a mailbox for test validation.
type MailBoxComponent struct {
	*network.Component
	RecvMailbox chan *protobuf.TestMessage
	SendMailbox chan *protobuf.TestMessage
}

// Startup creates a mailbox channel
func (state *MailBoxComponent) Startup(net *network.Network) {
	state.RecvMailbox = make(chan *protobuf.TestMessage, 100)
	state.SendMailbox = make(chan *protobuf.TestMessage, 100)
}

// Send puts a sent message into the SendMailbox channel
func (state *MailBoxComponent) Send(ctx *network.ComponentContext) error {
	switch msg := ctx.Message().(type) {
	case *protobuf.TestMessage:
		state.SendMailbox <- msg
	}
	return nil
}

// Receive puts a received message into the RecvMailbox channel
func (state *MailBoxComponent) Receive(ctx *network.ComponentContext) error {
	switch msg := ctx.Message().(type) {
	case *protobuf.TestMessage:
		state.RecvMailbox <- msg
	}
	return nil
}

func isIn(address string, ids ...peer.ID) bool {
	for _, a := range ids {
		if a.Address == address {
			return true
		}
	}
	return false
}

func isInAddress(address string, addresses ...string) bool {
	for _, a := range addresses {
		if a == address {
			return true
		}
	}
	return false
}

// Plugin for client test
type clientTestComponent struct {
	*network.Component
}

// Receive takes in *messages.ProxyMessage and replies with *messages.ID
func (p *clientTestComponent) Receive(ctx *network.ComponentContext) error {
	switch msg := ctx.Message().(type) {
	case *protobuf.TestMessage:
		response := &protobuf.TestMessage{Message: msg.Message}
		time.Sleep(time.Duration(msg.Duration) * time.Second)
		ctx.Reply(context.Background(), response)
	}

	return nil
}
