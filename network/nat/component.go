package nat

import (
	"net"
	"time"

	"github.com/fd/go-nat"
	"github.com/cocher/utils/log"
	"github.com/cocher/network"
	"github.com/cocher/peer"
)

type Component struct {
	*network.Component

	gateway nat.NAT

	internalIP net.IP
	externalIP net.IP

	internalPort int
	externalPort int
}

var (
	// ComponentID to reference NAT Component
	ComponentID                            = (*Component)(nil)
	_           network.ComponentInterface = (*Component)(nil)
)

func (p *Component) Startup(n *network.Network) {
	log.Infof("Setting up NAT traversal for address: %s", n.Address)

	info, err := network.ParseAddress(n.Address)
	if err != nil {
		return
	}

	p.internalPort = int(info.Port)

	gateway, err := nat.DiscoverGateway()
	if err != nil {
		log.Warnf("Unable to discover gateway: ", err)
		return
	}

	p.internalIP, err = gateway.GetInternalAddress()
	if err != nil {
		log.Warnf("Unable to fetch internal IP: ", err)
		return
	}

	p.externalIP, err = gateway.GetExternalAddress()
	if err != nil {
		log.Warnf("Unable to fetch external IP: ", err)
		return
	}

	log.Infof("Discovered gateway following the protocol %s.", gateway.Type())

	log.Info("Internal IP: ", p.internalIP.String())
	log.Info("External IP: ", p.externalIP.String())

	p.externalPort, err = gateway.AddPortMapping("tcp", p.internalPort, "noise", 1*time.Second)

	if err != nil {
		log.Warnf("Cannot setup port mapping: ", err)
		return
	}

	log.Infof("External port %d now forwards to your local port %d.", p.externalPort, p.internalPort)

	p.gateway = gateway

	info.Host = p.externalIP.String()
	info.Port = uint16(p.externalPort)

	// Set peer information based off of port mapping info.
	n.Address = info.String()
	n.ID = peer.CreateID(n.Address, n.GetKeys().PublicKey)

	log.Infof("Other peers may connect to you through the address %s.", n.Address)
}

func (p *Component) Cleanup(n *network.Network) {
	if p.gateway != nil {
		log.Info("Removing port binding...")

		err := p.gateway.DeletePortMapping("tcp", p.internalPort)
		if err != nil {
			log.Error(err)
		}
	}
}

// RegisterComponent registers a Component that automates port-forwarding of this nodes
// listening socket through any available UPnP interface.
//
// The Component is registered with a priority of -999999, and thus is executed first.
func RegisterComponent(builder *network.Builder) {
	builder.AddComponentWithPriority(-99999, new(Component))
}
