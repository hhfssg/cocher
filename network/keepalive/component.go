package keepalive

import (
	"context"
	"time"

	"github.com/cocher/utils/log"
	"github.com/cocher/internal/protobuf"
	"github.com/cocher/network"
)

const (
	DefaultKeepaliveInterval = 3 * time.Second
	DefaultKeepaliveTimeout  = 15 * time.Second
)

// Component is the keepalive Component
type Component struct {
	*network.Component

	// interval to send keepalive msg
	keepaliveInterval time.Duration
	// total keepalive timeout
	keepaliveTimeout time.Duration

	// Channel for peer network state change notification
	peerStateChan chan *PeerStateEvent
	stopCh        chan struct{}
	// map to save last state for a peer
	lastStates map[string]PeerState

	net *network.Network
}

type PeerState int

const (
	PEER_UNKNOWN     PeerState = 0 // peer network unknown
	PEER_UNREACHABLE PeerState = 1 // peer network unreachable
	PEER_REACHABLE   PeerState = 2 // peer network reachable
)

var stateString []string = []string{
	"unknown",
	"unreachable",
	"reachable",
}

type PeerStateEvent struct {
	Address string
	State   PeerState
}

// ComponentOption are configurable options for the keepalive Component
type ComponentOption func(*Component)

func WithKeepaliveTimeout(t time.Duration) ComponentOption {
	return func(o *Component) {
		o.keepaliveTimeout = t
	}
}

func WithKeepaliveInterval(t time.Duration) ComponentOption {
	return func(o *Component) {
		o.keepaliveInterval = t
	}
}

func WithPeerStateChan(c chan *PeerStateEvent) ComponentOption {
	return func(o *Component) {
		o.peerStateChan = c
	}
}

func defaultOptions() ComponentOption {
	return func(o *Component) {
		o.keepaliveInterval = DefaultKeepaliveInterval
		o.keepaliveTimeout = DefaultKeepaliveTimeout
	}
}

var (
	_ network.ComponentInterface = (*Component)(nil)
	// ComponentID is used to check existence of the keepalive Component
	ComponentID = (*Component)(nil)
)

// New returns a new keepalive Component with specified options
func New(opts ...ComponentOption) *Component {
	p := new(Component)
	defaultOptions()(p)

	p.stopCh = make(chan struct{})
	p.lastStates = make(map[string]PeerState)

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Startup implements the Component callback
func (p *Component) Startup(net *network.Network) {
	p.net = net

	// start keepalive service
	go p.keepaliveService()
}

func (p *Component) Cleanup(net *network.Network) {
	close(p.stopCh)
}

func (p *Component) PeerConnect(client *network.PeerClient) {
	p.updateLastStateAndNotify(client, PEER_REACHABLE)
}

func (p *Component) PeerDisconnect(client *network.PeerClient) {
	p.updateLastStateAndNotify(client, PEER_UNREACHABLE)
}

func (p *Component) Receive(ctx *network.ComponentContext) error {
	switch ctx.Message().(type) {
	case *protobuf.Keepalive:
		// Send keepalive response to peer.
		err := ctx.Reply(context.Background(), &protobuf.KeepaliveResponse{})

		if err != nil {
			return err
		}
	case *protobuf.KeepaliveResponse:
	}

	return nil
}

func (p *Component) keepaliveService() {
	t := time.NewTicker(p.keepaliveInterval)

	for {
		select {
		case <-t.C:
			// broadcast keepalive msg to all peers

			p.net.Broadcast(context.Background(), &protobuf.Keepalive{})
			p.timeout()
		case <-p.stopCh:
			t.Stop()
			break
		}
	}
}

// check all connetion if keepalive timeout
func (p *Component) timeout() {
	p.net.EachPeer(func(client *network.PeerClient) bool {
		// timeout notify state change
		if time.Now().After(client.Time.Add(p.keepaliveTimeout)) {
			p.updateLastStateAndNotify(client, PEER_UNREACHABLE)
		}
		return true
	})
}

func (p *Component) updateLastStateAndNotify(client *network.PeerClient, state PeerState) {
	last, ok := p.lastStates[client.Address]

	p.lastStates[client.Address] = state

	// no state change, no need notify
	if ok && last == state {
		return
	}

	if p.peerStateChan != nil {
		p.peerStateChan <- &PeerStateEvent{Address: client.Address, State: state}

		log.Infof("[keepalive] peerStateEvent: address %s state %s", client.Address, stateString[state])
	}
}
