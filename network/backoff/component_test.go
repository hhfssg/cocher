package backoff

import (
	"context"
	"flag"
	"testing"
	"time"

	"github.com/cocher/crypto"
	"github.com/cocher/crypto/ed25519"
	"github.com/cocher/examples/basic/messages"
	"github.com/cocher/network"
	"github.com/cocher/network/discovery"
	"github.com/cocher/types/opcode"
	"github.com/pkg/errors"
)

const (
	numNodes = 2
	protocol = "tcp"
	host     = "127.0.0.1"
)

var (
	idToPort = make(map[int]uint16)
	keys     = make(map[string]*crypto.KeyPair)
)

// mockComponent buffers all messages into a mailbox for this test.
type mockComponent struct {
	*network.Component
	Mailbox chan *messages.BasicMessage
}

// Startup implements the network interface callback
func (state *mockComponent) Startup(net *network.Network) {
	// Create mailbox
	state.Mailbox = make(chan *messages.BasicMessage, 1)
}

// Receive implements the network interface callback
func (state *mockComponent) Receive(ctx *network.ComponentContext) error {
	switch msg := ctx.Message().(type) {
	case *messages.BasicMessage:
		state.Mailbox <- msg
	}
	return nil
}

// broadcastAndCheck will send a message from node 0 to other nodes and check if it's received
func broadcastAndCheck(nodes []*network.Network, Components []*mockComponent) error {
	// Broadcast out a message from Node 0.
	expected := "This is a broadcasted message from Node 0."
	nodes[0].Broadcast(context.Background(), &messages.BasicMessage{Message: expected})

	// Check if message was received by other nodes.
	for i := 1; i < len(nodes); i++ {
		select {
		case received := <-Components[i].Mailbox:
			if received.Message != expected {
				return errors.Errorf("Expected message %s to be received by node %d but got %v\n", expected, i, received.Message)
			}
		case <-time.After(2 * time.Second):
			return errors.Errorf("Timed out attempting to receive message from Node 0.\n")
		}
	}

	return nil
}

// newNode creates a new node and and adds it to the cluster, allows adding certain Components if needed
func newNode(i int, addDiscoveryComponent bool, addBackoffComponent bool) (*network.Network, *mockComponent, error) {
	port := uint16(0)
	ok := false
	// get random port
	if port, ok = idToPort[i]; !ok {
		port = uint16(network.GetRandomUnusedPort())
		idToPort[i] = port
	}
	// restore the key if it was created in the past
	addr := network.FormatAddress(protocol, host, port)
	if _, ok := keys[addr]; !ok {
		keys[addr] = ed25519.RandomKeyPair()
	}

	builder := network.NewBuilder()
	builder.SetKeys(keys[addr])
	builder.SetAddress(addr)

	if addDiscoveryComponent {
		builder.AddComponent(new(discovery.Component))
	}
	if addBackoffComponent {
		builder.AddComponent(New())
	}

	Component := new(mockComponent)
	builder.AddComponent(Component)

	node, err := builder.Build()
	if err != nil {
		return nil, nil, err
	}

	go node.Listen()

	node.BlockUntilListening()

	// Bootstrap to Node 0
	if addDiscoveryComponent && i != 0 {
		node.Bootstrap(network.FormatAddress(protocol, host, uint16(idToPort[0])))
	}

	return node, Component, nil
}

// TestComponent tests the functionality of the exponential backoff as a Component.
func TestComponent(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping backoff Component test in short mode")
	}

	flag.Parse()

	var nodes []*network.Network
	var Components []*mockComponent

	opcode.RegisterMessageType(opcode.Opcode(1000), &messages.BasicMessage{})
	for i := 0; i < numNodes; i++ {
		node, Component, err := newNode(i, true, i == 0)
		if err != nil {
			t.Error(err)
		}
		Components = append(Components, Component)
		nodes = append(nodes, node)
	}

	// Wait for all nodes to finish discovering other peers.
	time.Sleep(1 * time.Second)

	// chack that broadcasts are working
	if err := broadcastAndCheck(nodes, Components); err != nil {
		t.Fatal(err)
	}

	// disconnect the node from the cluster
	nodes[1].Close()

	// wait until about the middle of the backoff period
	time.Sleep(defaultComponentInitialDelay + defaultMinInterval*2)

	// tests that broadcasting fails
	if err := broadcastAndCheck(nodes, Components); err == nil {
		t.Fatalf("On disconnect, expected the broadcast to fail")
	}

	// recreate the second node and add back to the cluster
	node, Component, err := newNode(1, false, false)
	if err != nil {
		t.Fatal(err)
	}
	nodes[1] = node
	Components[1] = Component

	// wait for reconnection
	time.Sleep(5 * time.Second)

	// broad cast should be working again
	if err := broadcastAndCheck(nodes, Components); err != nil {
		t.Fatal(err)
	}
}
