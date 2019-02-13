package keepalive

import (
	"testing"
	"time"

	"github.com/cocher/crypto/ed25519"
	"github.com/cocher/network"
	"github.com/stretchr/testify/assert"
)

const (
	numNodes = 2
	protocol = "tcp"
	host     = "127.0.0.1"
)

var stateBuf []*PeerStateEvent

func newNet(port uint16, keepalive bool, ch chan *PeerStateEvent, peer string) (*network.Network, error) {

	keys := ed25519.RandomKeyPair()

	builder := network.NewBuilder()
	builder.SetKeys(keys)
	builder.SetAddress(network.FormatAddress(protocol, host, port))

	if keepalive {
		go func(ch chan *PeerStateEvent) {
			var state *PeerStateEvent
			for {
				select {
				case state = <-ch:
					stateBuf = append(stateBuf, state)
				}
			}
		}(ch)

		builder.AddComponent(New(WithPeerStateChan(ch)))
	}

	net, err := builder.Build()
	if err != nil {
		return nil, err
	}

	go net.Listen()

	if peer != "" {
		net.Bootstrap(peer)
	}

	return net, nil
}

// test the keepalive state event notification when one peer does not responde to keepalive
func TestKeepaliveTimeout(t *testing.T) {
	t.Parallel()

	stateChan := make(chan *PeerStateEvent)
	_, err := newNet(3000, true, stateChan, "")
	if err != nil {
		t.Fatal("failed to create network")
	}

	peer2Addr := protocol + "://" + host + ":" + "3001"
	_, err = newNet(3001, false, nil, protocol+"://"+host+":"+"3000")
	if err != nil {
		t.Fatal("failed to create network")
	}

	// sleep enough for the default timeout for peer2
	time.Sleep(20 * time.Second)

	assert.Equal(t, 2, len(stateBuf))

	assert.Equal(t, peer2Addr, stateBuf[0].Address)
	assert.Equal(t, PEER_REACHABLE, stateBuf[0].State)

	assert.Equal(t, peer2Addr, stateBuf[1].Address)
	assert.Equal(t, PEER_UNREACHABLE, stateBuf[1].State)
}

// test the keepalive state event notification when one peer close the network
func TestKeepaliveClose(t *testing.T) {
	t.Parallel()

	stateChan := make(chan *PeerStateEvent)
	_, err := newNet(3000, true, stateChan, "")
	if err != nil {
		t.Fatal("failed to create network")
	}

	peer2Addr := protocol + "://" + host + ":" + "3001"
	net2, err := newNet(3001, true, nil, protocol+"://"+host+":"+"3000")
	if err != nil {
		t.Fatal("failed to create network")
	}

	//wait 5s for the stable connection
	time.Sleep(5 * time.Second)

	net2.Close()

	time.Sleep(2 * time.Second)

	assert.Equal(t, 2, len(stateBuf))

	assert.Equal(t, peer2Addr, stateBuf[0].Address)
	assert.Equal(t, PEER_REACHABLE, stateBuf[0].State)

	assert.Equal(t, peer2Addr, stateBuf[1].Address)
	assert.Equal(t, PEER_UNREACHABLE, stateBuf[1].State)
}
