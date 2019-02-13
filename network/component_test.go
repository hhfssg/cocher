package network

import (
	"fmt"
	"testing"

	"github.com/cocher/crypto/ed25519"
	"github.com/stretchr/testify/assert"

	"github.com/uber-go/atomic"
)

type MockComponent struct {
	*Component

	startup        atomic.Int32
	receive        atomic.Int32
	cleanup        atomic.Int32
	peerConnect    atomic.Int32
	peerDisconnect atomic.Int32
}

func (p *MockComponent) Startup(net *Network) {
	p.startup.Inc()
}

func (p *MockComponent) Receive(ctx *ComponentContext) error {
	p.receive.Inc()
	return nil
}

func (p *MockComponent) Cleanup(net *Network) {
	p.cleanup.Inc()
}

func (p *MockComponent) PeerConnect(client *PeerClient) {
	p.peerConnect.Inc()
}

func (p *MockComponent) PeerDisconnect(client *PeerClient) {
	p.peerDisconnect.Inc()
}

func TestComponentHooks(t *testing.T) {
	host := "localhost"
	var nodes []*Network
	nodeCount := 4

	for i := 0; i < nodeCount; i++ {
		builder := NewBuilder()
		builder.SetKeys(ed25519.RandomKeyPair())
		builder.SetAddress(FormatAddress("tcp", host, uint16(GetRandomUnusedPort())))
		builder.AddComponent(new(MockComponent))

		node, err := builder.Build()
		if err != nil {
			fmt.Println(err)
		}

		go node.Listen()

		nodes = append(nodes, node)
	}

	for _, node := range nodes {
		node.BlockUntilListening()
	}

	//for i, node := range nodes {
	//	if i != 0 {
	//		node.Bootstrap(nodes[0].Address)
	//	}
	//}
	//
	//time.Sleep(500 * time.Millisecond)
	//
	//for _, node := range nodes {
	//	node.Close()
	//}
	//
	//time.Sleep(500 * time.Millisecond)
	//
	//if startup != nodeCount {
	//	t.Fatalf("startup hooks error, got: %d, expected: %d", startup, nodeCount)
	//}
	//
	//if receive < nodeCount*2 { // Cannot in specific time
	//	t.Fatalf("receive hooks error, got: %d, expected at least: %d", receive, nodeCount*2)
	//}
	//
	//if cleanup != nodeCount {
	//	t.Fatalf("cleanup hooks error, got: %d, expected: %d", cleanup, nodeCount)
	//}
	//
	//if peerConnect < nodeCount*2 {
	//	t.Fatalf("connect hooks error, got: %d, expected at least: %d", peerConnect, nodeCount*2)
	//}
	//
	//if peerDisconnect < nodeCount*2 {
	//	t.Fatalf("disconnect hooks error, got: %d, expected at least: %d", peerDisconnect, nodeCount*2)
	//}
}

func TestRegisterComponent(t *testing.T) {
	t.Parallel()

	ComponentID := (*Component)(nil)

	b := NewBuilder()
	b.AddComponentWithPriority(-99999, new(Component))

	n, err := b.Build()
	assert.Equal(t, nil, err)

	p, ok := n.Components.Get(ComponentID)
	assert.Equal(t, true, ok)

	Component := p.(*Component)
	assert.NotEqual(t, nil, Component)
}
