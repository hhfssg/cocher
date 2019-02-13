package addressmap

import (
	"github.com/cocher/utils/log"
	"github.com/cocher/network"
	"github.com/cocher/peer"
)

type Component struct {
	*network.Component

	MappingAddress string
}

var (
	// ComponentID to reference address mapping Component
	ComponentID                            = (*Component)(nil)
	_           network.ComponentInterface = (*Component)(nil)
)

func (p *Component) Startup(n *network.Network) {
	log.Infof("Setting up address mapping for address: %s", n.Address)

	info, err := network.ParseAddress(n.Address)
	if err != nil {
		log.Errorf("error parsing network address %s", n.Address)
		return
	}

	mapInfo, err := network.ParseAddress(p.MappingAddress)
	if err != nil {
		log.Errorf("error parsing mapping address %s", p.MappingAddress)
		return
	}

	log.Infof("update mapping address from %s to %s", info.String(), mapInfo.String())

	n.Address = mapInfo.String()
	n.ID = peer.CreateID(n.Address, n.GetKeys().PublicKey)
}
