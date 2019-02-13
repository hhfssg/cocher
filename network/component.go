package network

// ComponentInterface is used to proxy callbacks to a particular Component instance.
type ComponentInterface interface {
	// Callback for when the network starts listening for peers.
	Startup(net *Network)

	// Callback for when an incoming message is received. Return true
	// if the Component will intercept messages to be processed.
	Receive(ctx *ComponentContext) error

	// Callback for when the network stops listening for peers.
	Cleanup(net *Network)

	// Callback for when a peer connects to the network.
	PeerConnect(client *PeerClient)

	// Callback for when a peer disconnects from the network.
	PeerDisconnect(client *PeerClient)
}

// Component is an abstract class which all Components extend.
type Component struct{}

// Hook callbacks of network builder Components

// Startup is called only once when the Component is loaded
func (*Component) Startup(net *Network) {}

// Receive is called every time when messages are received
func (*Component) Receive(ctx *ComponentContext) error { return nil }

// Cleanup is called only once after network stops listening
func (*Component) Cleanup(net *Network) {}

// PeerConnect is called every time a PeerClient is initialized and connected
func (*Component) PeerConnect(client *PeerClient) {}

// PeerDisconnect is called every time a PeerClient connection is closed
func (*Component) PeerDisconnect(client *PeerClient) {}
