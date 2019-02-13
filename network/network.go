package network

import (
	"bufio"
	"context"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cocher/crypto"
	"github.com/cocher/internal/protobuf"
	"github.com/cocher/network/transport"
	"github.com/cocher/peer"
	"github.com/cocher/types/opcode"

	"os"
	"os/signal"
	"syscall"

	"github.com/gogo/protobuf/proto"
	"github.com/cocher/utils/log"
	"github.com/pkg/errors"
)

type writeMode int

const (
	WRITE_MODE_LOOP writeMode = iota
	WRITE_MODE_DIRECT
)

const (
	defaultConnectionTimeout = 60 * time.Second
	defaultReceiveWindowSize = 4096
	defaultSendWindowSize    = 4096
	defaultWriteBufferSize   = 4096
	defaultRecvBufferSize    = 4 * 1024 * 1024
	defaultWriteFlushLatency = 50 * time.Millisecond
	defaultWriteTimeout      = 3 * time.Second
	defaultWriteMode         = WRITE_MODE_LOOP
)

var contextPool = sync.Pool{
	New: func() interface{} {
		return new(ComponentContext)
	},
}

var (
	_ NetworkInterface = (*Network)(nil)
)

// Network represents the current networking state for this node.
type Network struct {
	netID uint32

	opts options

	// Node's keypair.
	keys *crypto.KeyPair

	// Full address to listen on. `protocol://host:port`
	Address string

	// Map of Components registered to the network.
	// map[string]Component
	Components *ComponentList

	// Node's cryptographic ID.
	ID peer.ID

	// Map of real remote ip:port (key) with real local ip:port(value);
	// especially, when a udp client call server, local port is dynamic,
	// so we need to record the real port;
	udpDialAddrs *sync.Map

	// Map of connection addresses (string) <-> *network.PeerClient
	// so that the Network doesn't dial multiple times to the same ip
	peers *sync.Map

	//RecvQueue chan *protobuf.Message

	// Map of connection addresses (string) <-> *ConnState
	connections *sync.Map

	// Map of protocol addresses (string) <-> *transport.Layer
	transports *sync.Map

	// listeningCh will block a goroutine until this node is listening for peers.
	listeningCh chan struct{}

	// <-kill will begin the server shutdown process
	kill chan struct{}
}

// options for network struct
type options struct {
	connectionTimeout time.Duration
	signaturePolicy   crypto.SignaturePolicy
	hashPolicy        crypto.HashPolicy
	recvWindowSize    int
	sendWindowSize    int
	writeBufferSize   int
	recvBufferSize    int
	writeFlushLatency time.Duration
	writeTimeout      time.Duration
	writeMode         writeMode
}

// ConnState represents a connection.
type ConnState struct {
	conn         interface{}
	writer       *bufio.Writer
	messageNonce uint64
	writerMutex  *sync.Mutex
	IsDial       bool // when dial out to server on udp condition, value is true; otherwise, it is false;
}

// Init starts all network I/O workers.
func (n *Network) Init() {
	// Spawn write flusher.
	go n.flushLoop()
	go n.waitExit()
}

func (n *Network) waitExit() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	select {
	case sig := <-sigs:
		log.Infof("Network received exit signal:%v.", sig.String())
		n.Close()
		os.Exit(0)
	}
}

func (n *Network) flushLoop() {
	t := time.NewTicker(n.opts.writeFlushLatency)
	defer t.Stop()
	for {
		select {
		case <-n.kill:
			return
		case <-t.C:
			n.connections.Range(func(key, value interface{}) bool {
				if state, ok := value.(*ConnState); ok {
					state.writerMutex.Lock()
					if err := state.writer.Flush(); err != nil {
						log.Warnf(err.Error())
					}
					state.writerMutex.Unlock()
				}
				return true
			})
		}
	}
}

// GetKeys returns the keypair for this network
func (n *Network) GetKeys() *crypto.KeyPair {
	return n.keys
}

func (n *Network) dispatchMessage(client *PeerClient, msg *protobuf.Message) {
	if !client.IsIncomingReady() {
		return
	}

	var ptr proto.Message
	// unmarshal message based on specified opcode
	code := opcode.Opcode(msg.Opcode)
	switch code {
	case opcode.BytesCode:
		ptr = &protobuf.Bytes{}
	case opcode.PingCode:
		ptr = &protobuf.Ping{}
	case opcode.PongCode:
		ptr = &protobuf.Pong{}
	case opcode.LookupNodeRequestCode:
		ptr = &protobuf.LookupNodeRequest{}
	case opcode.LookupNodeResponseCode:
		ptr = &protobuf.LookupNodeResponse{}
	case opcode.DisconnectCode:
		ptr = &protobuf.Disconnect{}
	case opcode.UnregisteredCode:
		log.Error("network: message received had no opcode")
		return
	default:
		var err error
		ptr, err = opcode.GetMessageType(code)
		if err != nil {
			log.Error("network: received message opcode is not registered")
			return
		}
	}
	if len(msg.Message) > 0 {
		if err := proto.Unmarshal(msg.Message, ptr); err != nil {
			log.Error(err)
			return
		}
	}

	client.Time = time.Now()

	if msg.RequestNonce > 0 && msg.ReplyFlag {
		if _state, exists := client.Requests.Load(msg.RequestNonce); exists {
			state := _state.(*RequestState)
			select {
			case state.data <- ptr:
			case <-state.closeSignal:
			}
			return
		}
	}

	switch msgRaw := ptr.(type) {
	case *protobuf.Bytes:
		client.handleBytes(msgRaw.Data)
	default:
		ctx := contextPool.Get().(*ComponentContext)
		ctx.client = client
		ctx.message = msgRaw
		ctx.nonce = msg.RequestNonce

		go func() {
			// Execute 'on receive message' callback for all Components.
			n.Components.Each(func(Component ComponentInterface) {
				if err := Component.Receive(ctx); err != nil {
					log.Errorf("%+v", err)
				}
			})

			contextPool.Put(ctx)
		}()
	}
}

// Listen starts listening for peers on a port.
func (n *Network) Listen() {

	// Handle 'network starts listening' callback for Components.
	n.Components.Each(func(Component ComponentInterface) {
		Component.Startup(n)
	})

	// Handle 'network stops listening' callback for Components.
	defer func() {
		n.Components.Each(func(Component ComponentInterface) {
			Component.Cleanup(n)
		})
	}()

	addrInfo, err := ParseAddress(n.Address)
	if err != nil {
		log.Fatal(err)
	}

	var listener interface{}
	if t, exists := n.transports.Load(addrInfo.Protocol); exists {
		listener, err = t.(transport.Layer).Listen(int(addrInfo.Port))
		if err != nil {
			log.Fatal(err)
		}
	} else {
		log.Fatal("invalid protocol: " + addrInfo.Protocol)
	}

	n.startListening()

	log.Infof("Listening for peers on %s.", n.Address)

	// handle server shutdowns
	go func() {
		select {
		case <-n.kill:
			// cause listener.Accept() to stop blocking so it can continue the loop
			if addrInfo.Protocol == "tcp" || addrInfo.Protocol == "kcp" {
				listener.(net.Listener).Close()
			}
			if addrInfo.Protocol == "udp" {
				udpConn, _ := listener.(*net.UDPConn)
				udpConn.Close()
			}
		}
	}()

	// Handle new clients.
	switch addrInfo.Protocol {
	case "tcp", "kcp":
		for {
			if conn, err := listener.(net.Listener).Accept(); err == nil {
				go n.Accept(conn)

			} else {
				// if the Shutdown flag is set, no need to continue with the for loop
				select {
				case <-n.kill:
					log.Infof("Shutting down server on %s.", n.Address)
					return
				default:
					log.Error(err)
				}
			}
		}
	case "udp":
		go n.AcceptUdp(listener.(*net.UDPConn))
	default:
		log.Fatal("invalid protocol: " + addrInfo.Protocol)
	}

}

// getOrSetPeerClient either returns a cached peer client or creates a new one given a net.Conn
// or dials the client if no net.Conn is provided.
func (n *Network) getOrSetPeerClient(address string, conn interface{}) (*PeerClient, error) {
	address, err := ToUnifiedAddress(address)
	if err != nil {
		return nil, err
	}

	if address == n.Address {
		return nil, errors.New("network: peer should not dial itself")
	}

	addrInfo, err := ParseAddress(address)
	if err != nil {
		return nil, err
	}
	// if conn is not nil, check that the sender host matches the net.Conn remote host address
	//	todo: delete the following statement on if conn!=nil{...}
	if conn != nil {
		var remoteAddrInfo *AddressInfo
		switch addrInfo.Protocol {
		case "tcp", "kcp":
			remoteAddrInfo, err = ParseAddress(fmt.Sprintf("%s://%s", conn.(net.Conn).RemoteAddr().Network(), conn.(net.Conn).RemoteAddr().String()))
		case "udp":
			//udpConn, _ := conn.(*net.UDPConn)
			//remoteAddrInfo, err = ParseAddress(fmt.Sprintf("%s://%s", udpConn.RemoteAddr().Network(), udpConn.RemoteAddr().String()))
		default:
			log.Fatal("invalid protocol: " + addrInfo.Protocol)

		}
		if err != nil {
			return nil, err
		}

		// disalbe checking for address matching to pass for the case when ip mapping address is used
		// to be further checked for impact
		if (addrInfo.Protocol == "tcp" || addrInfo.Protocol == "kcp") && addrInfo.Host != remoteAddrInfo.Host {
			//return nil, errors.New("network: sender address did not match connection remote address")
		}
	}

	clientNew, err := createPeerClient(n, address)
	if err != nil {
		return nil, err
	}

	c, exists := n.peers.LoadOrStore(address, clientNew)
	if exists {
		client := c.(*PeerClient)

		if !client.IsOutgoingReady() {
			return nil, errors.New("network: peer failed to connect")
		}

		return client, nil
	}

	client := c.(*PeerClient)
	defer func() {
		client.setOutgoingReady()
	}()
	isDial := false
	if conn == nil {
		conn, err = n.Dial(address)
		isDial = true
		if err != nil {
			n.peers.Delete(address)
			return nil, err
		}
	}
	if addrInfo.Protocol == "tcp" || addrInfo.Protocol == "kcp" {
		netConn, _ := conn.(net.Conn)
		n.connections.Store(address, &ConnState{
			conn:        conn,
			writer:      bufio.NewWriterSize(netConn, n.opts.writeBufferSize),
			writerMutex: new(sync.Mutex),
		})
	}
	if addrInfo.Protocol == "udp" {
		udpConn, _ := conn.(*net.UDPConn)
		n.connections.Store(address, &ConnState{
			conn:        conn,
			writer:      bufio.NewWriterSize(udpConn, n.opts.writeBufferSize),
			writerMutex: new(sync.Mutex),
			IsDial:      isDial,
		})
		n.udpDialAddrs.Store(address, udpConn.LocalAddr().String())
	}
	client.Init()

	client.setIncomingReady()
	return client, nil
}

// Client either creates or returns a cached peer client given its host address.
func (n *Network) Client(address string) (*PeerClient, error) {
	return n.getOrSetPeerClient(address, nil)
}

// ConnectionStateExists returns true if network has a connection on a given address.
func (n *Network) ConnectionStateExists(address string) bool {
	_, ok := n.connections.Load(address)
	return ok
}

// ConnectionState returns a connections state for current address.
func (n *Network) ConnectionState(address string) (*ConnState, bool) {
	conn, ok := n.connections.Load(address)
	if !ok {
		return nil, false
	}
	return conn.(*ConnState), true
}

// startListening will start node for listening for new peers.
func (n *Network) startListening() {
	close(n.listeningCh)
}

// BlockUntilListening blocks until this node is listening for new peers.
func (n *Network) BlockUntilListening() {
	<-n.listeningCh
}

// Bootstrap with a number of peers and commence a handshake.
func (n *Network) Bootstrap(addresses ...string) {
	n.BlockUntilListening()

	addresses = FilterPeers(n.Address, addresses)

	for _, address := range addresses {
		client, err := n.Client(address)

		if err != nil {
			log.Error(err)
			continue
		}

		err = client.Tell(context.Background(), &protobuf.Ping{})
		if err != nil {
			continue
		}
	}
}

// Dial establishes a bidirectional connection to an address, and additionally handshakes with said address.
func (n *Network) Dial(address string) (interface{}, error) {
	addrInfo, err := ParseAddress(address)
	if err != nil {
		return nil, err
	}

	if addrInfo.Host != "127.0.0.1" {
		host, err := ParseAddress(n.Address)
		if err != nil {
			return nil, err
		}
		// check if dialing address is same as its own IP
		if addrInfo.Host == host.Host {
			addrInfo.Host = "127.0.0.1"
		}
	}

	// Choose scheme.
	t, exists := n.transports.Load(addrInfo.Protocol)
	if !exists {
		log.Fatal("invalid protocol: " + addrInfo.Protocol)
	}

	var conn interface{}
	conn, err = t.(transport.Layer).Dial(addrInfo.HostPort())
	if err != nil {
		return nil, err
	}

	// use the connection for also receiving messages
	if addrInfo.Protocol == "udp" {
		go n.AcceptUdp(conn)
	}
	if addrInfo.Protocol == "tcp" || addrInfo.Protocol == "kcp" {
		go n.Accept(conn)
	}

	return conn, nil
}

// Accept handles peer registration and processes incoming message streams.
func (n *Network) Accept(incoming interface{}) {
	var client *PeerClient
	var clientInit sync.Once

	recvWindow := NewRecvWindow(n.opts.recvWindowSize)

	// Cleanup connections when we are done with them.
	defer func() {
		time.Sleep(1 * time.Second)

		if client != nil {
			client.Close()
		}

		if incoming != nil {
			incoming.(net.Conn).Close()
		}
	}()

	for {
		msg, err := n.receiveMessage(incoming)
		if err != nil {
			if err != errEmptyMsg {
				log.Error(err)
			}
			break
		}

		// Initialize client if not exists.
		clientInit.Do(func() {
			client, err = n.getOrSetPeerClient(msg.Sender.Address, incoming)
			if err != nil {
				return
			}

			client.ID = (*peer.ID)(msg.Sender)

			if !n.ConnectionStateExists(client.ID.Address) {
				err = errors.New("network: failed to load session")
			}

			//client.setIncomingReady() has done in getOrSetPeerClient() function
		})

		if err != nil {
			log.Error(err)
			return
		}

		go func() {
			if msg.Signature != nil && !crypto.Verify(
				n.opts.signaturePolicy,
				n.opts.hashPolicy,
				msg.Sender.NetKey,
				SerializeMessage(msg.Sender, msg.Message),
				msg.Signature,
			) {
				log.Errorf("received message had an malformed signature")
				return
			}
			// Peer sent message with a completely different ID. Disconnect.
			if !client.ID.Equals(peer.ID(*msg.Sender)) {
				log.Errorf("message signed by peer %s but client is %s", peer.ID(*msg.Sender), client.ID.Address)
				return
			}

			recvWindow.Push(msg.MessageNonce, msg)

			ready := recvWindow.Pop()
			for _, msg := range ready {
				msg := msg
				client.Submit(func() {
					n.dispatchMessage(client, msg.(*protobuf.Message))
				})
			}
		}()
	}
}

// Accept handles peer registration and processes incoming message streams.
func (n *Network) AcceptUdp(incoming interface{}) {
	var client *PeerClient

	// Cleanup connections when we are done with them.
	defer func() {
		time.Sleep(1 * time.Second)

		if client != nil {
			client.Close()
		}

		if incoming != nil {
			udpConn, _ := incoming.(*net.UDPConn)
			udpConn.Close()
		}
	}()

	for {
		msg, err := n.receiveUDPMessage(incoming)
		if err != nil {
			if err != errEmptyMsg {
				log.Error(err)
			}
			break
		}
		go func() {
			if msg.Signature != nil && !crypto.Verify(
				n.opts.signaturePolicy,
				n.opts.hashPolicy,
				msg.Sender.NetKey,
				SerializeMessage(msg.Sender, msg.Message),
				msg.Signature,
			) {
				log.Error("received message had an malformed signature")
				return
			}

			client, err = n.getOrSetPeerClient(msg.DialAddress, incoming)
			if err != nil {
				log.Error(err)
				return
			}

			client.ID = (*peer.ID)(msg.Sender)

			if !n.ConnectionStateExists(msg.DialAddress) {
				log.Error(errors.New("network: failed to load session"))
				return
			}

			// Peer sent message with a completely different ID. Disconnect.
			if !client.ID.Equals(peer.ID(*msg.Sender)) {
				log.Errorf("message signed by peer %s but client is %s", peer.ID(*msg.Sender), client.ID.Address)
				return
			}
			client.Submit(func() {
				n.dispatchMessage(client, msg)
			})
		}()
	}
}

// Component returns a Components proxy interface should it be registered with the
// network. The second returning parameter is false otherwise.
//
// Example: network.Component((*Component)(nil))
func (n *Network) Component(key interface{}) (ComponentInterface, bool) {
	return n.Components.Get(key)
}

// PrepareMessage marshals a message into a *protobuf.Message and signs it with this
// nodes private key. Errors if the message is null.
func (n *Network) PrepareMessage(ctx context.Context, message proto.Message) (*protobuf.Message, error) {
	if message == nil {
		return nil, errors.New("network: message is null")
	}

	opcode, err := opcode.GetOpcode(message)
	if err != nil {
		return nil, err
	}

	raw, err := proto.Marshal(message)
	if err != nil {
		return nil, err
	}

	id := protobuf.ID(n.ID)

	msg := &protobuf.Message{
		Message: raw,
		Opcode:  uint32(opcode),
		Sender:  &id,
	}

	if GetSignMessage(ctx) {
		signature, err := n.keys.Sign(
			n.opts.signaturePolicy,
			n.opts.hashPolicy,
			SerializeMessage(&id, raw),
		)
		if err != nil {
			return nil, err
		}
		msg.Signature = signature
	}
	return msg, nil
}

// Write asynchronously sends a message to a denoted target address.
func (n *Network) Write(address string, message *protobuf.Message) error {
	state, ok := n.ConnectionState(address)
	if !ok {
		return errors.New("network: connection does not exist")
	}

	message.MessageNonce = atomic.AddUint64(&state.messageNonce, 1)

	addrInfo, err := ParseAddress(n.Address)
	if err != nil {
		log.Fatal(err)
	}
	if addrInfo.Protocol == "tcp" || addrInfo.Protocol == "kcp" {
		tcpConn, _ := state.conn.(net.Conn)
		tcpConn.SetWriteDeadline(time.Now().Add(n.opts.writeTimeout))
		err = n.sendMessage(state.writer, message, state.writerMutex, state)
		if err != nil {
			return err
		}
	}
	if addrInfo.Protocol == "udp" {
		udpConn, _ := state.conn.(*net.UDPConn)
		udpConn.SetWriteDeadline(time.Now().Add(n.opts.writeTimeout))
		err = n.sendUDPMessage(state.writer, message, state.writerMutex, state, address)
		if err != nil {
			return err
		}
	}

	if n.opts.writeMode == WRITE_MODE_DIRECT {
		state.writerMutex.Lock()
		if err := state.writer.Flush(); err != nil {
			log.Warnf(err.Error())
		}
		state.writerMutex.Unlock()
	}
	return nil
}

// Broadcast asynchronously broadcasts a message to all peer clients.
func (n *Network) Broadcast(ctx context.Context, message proto.Message) {
	signed, err := n.PrepareMessage(ctx, message)
	if err != nil {
		log.Errorf("network: failed to broadcast message")
		return
	}

	n.EachPeer(func(client *PeerClient) bool {
		err := n.Write(client.Address, signed)
		if err != nil {
			log.Warnf("failed to send message to peer %v [err=%s]", client.ID, err)
		}
		return true
	})
}

// BroadcastByAddresses broadcasts a message to a set of peer clients denoted by their addresses.
func (n *Network) BroadcastByAddresses(ctx context.Context, message proto.Message, addresses ...string) {
	signed, err := n.PrepareMessage(ctx, message)
	if err != nil {
		return
	}

	for _, address := range addresses {
		n.Write(address, signed)
	}
}

// BroadcastByIDs broadcasts a message to a set of peer clients denoted by their peer IDs.
func (n *Network) BroadcastByIDs(ctx context.Context, message proto.Message, ids ...peer.ID) {
	signed, err := n.PrepareMessage(ctx, message)
	if err != nil {
		return
	}

	for _, id := range ids {
		n.Write(id.Address, signed)
	}
}

// BroadcastRandomly asynchronously broadcasts a message to random selected K peers.
// Does not guarantee broadcasting to exactly K peers.
func (n *Network) BroadcastRandomly(ctx context.Context, message proto.Message, K int) {
	var addresses []string

	n.EachPeer(func(client *PeerClient) bool {
		addresses = append(addresses, client.Address)

		// Limit total amount of addresses in case we have a lot of peers.
		return len(addresses) <= K*3
	})

	// Flip a coin and shuffle :).
	rand.Shuffle(len(addresses), func(i, j int) {
		addresses[i], addresses[j] = addresses[j], addresses[i]
	})

	if len(addresses) < K {
		K = len(addresses)
	}

	n.BroadcastByAddresses(ctx, message, addresses[:K]...)
}

// Close shuts down the entire network.
func (n *Network) Close() {
	close(n.kill)

	n.EachPeer(func(client *PeerClient) bool {
		// tell remote endpoint Disconnect MSG: 'I am going to leave, please release yourself's resource'
		client.Tell(context.Background(), &protobuf.Disconnect{})
		client.Close()
		return true
	})
}

func (n *Network) EachPeer(fn func(client *PeerClient) bool) {
	n.peers.Range(func(_, value interface{}) bool {
		client := value.(*PeerClient)
		return fn(client)
	})
}

func (n *Network) SetNetworkID(netID uint32) {
	n.netID = netID
}

func (n *Network) GetNetworkID() uint32 {
	return n.netID
}
