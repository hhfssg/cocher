package main

import (
	"flag"
	"io"
	"net"
	"strings"
	"time"

	"github.com/cocher/utils/log"
	"github.com/cocher/crypto/ed25519"
	"github.com/cocher/network"
	"github.com/cocher/network/discovery"
	"github.com/xtaci/smux"
)

func muxStreamConfig() *smux.Config {
	config := smux.DefaultConfig()
	config.KeepAliveTimeout = 30 * time.Second
	config.KeepAliveInterval = 5 * time.Second

	return config
}

func proxy(a, b io.ReadWriter) {
	ch1 := make(chan struct{})
	ch2 := make(chan struct{})

	go func() {
		io.Copy(a, b)
		close(ch1)
	}()
	go func() {
		io.Copy(b, a)
		close(ch2)
	}()
	select {
	case <-ch1:
	case <-ch2:
	}
}

type ExampleServerComponent struct {
	network.Component
	remoteAddress string
}

func (state *ExampleServerComponent) PeerConnect(client *network.PeerClient) {
	log.Infof("New connection from %s.", client.Address)

	go state.handleClient(client)
}

func (state *ExampleServerComponent) handleClient(client *network.PeerClient) {
	session, err := smux.Server(client, muxStreamConfig())
	if err != nil {
		log.Fatal(err)
	}
	for {
		stream, err := session.AcceptStream()
		if err != nil {
			log.Error(err)
			break
		}

		log.Infof("New incoming stream from %s.", client.Address)

		go func() {
			defer stream.Close()

			remote, err := net.Dial("tcp", state.remoteAddress)
			if err != nil {
				log.Error(err)
				return
			}
			defer remote.Close()

			proxy(stream, remote)
		}()
	}
}

func (state *ExampleServerComponent) PeerDisconnect(client *network.PeerClient) {
	log.Infof("Lost connection with %s.", client.Address)
}

type ProxyServerComponent struct {
	network.Component
	listenAddress string
}

func (state *ProxyServerComponent) PeerConnect(client *network.PeerClient) {
	log.Infof("Connected to proxy destination %s.", client.Address)

	go state.startProxying(client)
}

func (state *ProxyServerComponent) startProxying(client *network.PeerClient) {
	session, err := smux.Client(client, muxStreamConfig())
	if err != nil {
		log.Fatal(err)
	}

	// Open proxy server.
	listener, err := net.Listen("tcp", state.listenAddress)
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Fatal(err)
		}

		log.Infof("Proxying data from %s to %s.", conn.RemoteAddr().String(), client.Address)

		go func() {
			defer conn.Close()

			remote, err := session.OpenStream()
			if err != nil {
				log.Error(err)
				return
			}
			defer remote.Close()

			proxy(conn, remote)
		}()
	}
}

func (state *ProxyServerComponent) PeerDisconnect(client *network.PeerClient) {
	log.Infof("Lost connection with proxy destination %s.", client.Address)
}

// An example showcasing how to use streams in Noise by creating a sample proxying server.
func main() {
	flag.Set("logtostderr", "true")

	portFlag := flag.Int("port", 3000, "port to listen to")
	hostFlag := flag.String("host", "localhost", "host to listen to")
	protocolFlag := flag.String("protocol", "tcp", "protocol to use (kcp/tcp)")
	peersFlag := flag.String("peers", "", "peers to connect to")
	modeFlag := flag.String("mode", "server", "mode to use (server/client)")
	addressFlag := flag.String("address", "127.0.0.1:80", "port forwarding connect/listen address")
	flag.Parse()

	port := uint16(*portFlag)
	host := *hostFlag
	protocol := *protocolFlag
	mode := *modeFlag
	address := *addressFlag
	peers := strings.Split(*peersFlag, ",")

	keys := ed25519.RandomKeyPair()

	log.Infof("Private Key: %s", keys.PrivateKeyHex())
	log.Infof("Public Key: %s", keys.PublicKeyHex())

	builder := network.NewBuilder()
	builder.SetKeys(keys)
	builder.SetAddress(network.FormatAddress(protocol, host, port))

	// Register peer discovery Component.
	builder.AddComponent(new(discovery.Component))

	// Add custom port forwarding Component.
	if mode == "server" {
		builder.AddComponent(&ExampleServerComponent{remoteAddress: address})
	} else if mode == "client" {
		builder.AddComponent(&ProxyServerComponent{listenAddress: address})
	}

	net, err := builder.Build()
	if err != nil {
		log.Fatal(err)
		return
	}

	go net.Listen()

	if len(peers) > 0 {
		net.Bootstrap(peers...)
	}

	select {}
}
