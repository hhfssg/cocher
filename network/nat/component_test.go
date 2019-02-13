package nat

import (
	"testing"
	"time"

	"github.com/cocher/network"
	"github.com/cocher/network/discovery"

	"github.com/stretchr/testify/assert"
)

func TestNatConnect(t *testing.T) {
	t.Parallel()

	numNodes := 2
	nodes := make([]*network.Network, 0)
	for i := 0; i < numNodes; i++ {
		b := network.NewBuilder()
		port := network.GetRandomUnusedPort()
		b.SetAddress(network.FormatAddress("tcp", "localhost", uint16(port)))
		RegisterComponent(b)
		b.AddComponent(new(discovery.Component))
		n, err := b.Build()
		go n.Listen()

		assert.Equal(t, nil, err)
		nodes = append(nodes, n)
		n.BlockUntilListening()
	}

	nodes[1].Bootstrap(nodes[0].Address)
	ComponentInt, ok := nodes[1].Component(discovery.ComponentID)
	assert.Equal(t, true, ok)
	Component := ComponentInt.(*discovery.Component)
	routes := Component.Routes
	peers := routes.GetPeers()
	for len(peers) < numNodes-1 {
		peers = routes.GetPeers()
		time.Sleep(50 * time.Millisecond)
	}

	assert.Equal(t, len(peers), 1)
}
