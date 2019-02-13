package network

import (
	"io"
	"sync"

	"net"

	"encoding/binary"

	"strings"

	"fmt"

	"github.com/gogo/protobuf/proto"
	"github.com/cocher/utils/log"
	"github.com/cocher/internal/protobuf"
	"github.com/pkg/errors"
)

const MAX_PACKAGE_SIZE = 1024 * 64

// sendMessage marshals, signs and sends a message over a stream.
func (n *Network) sendUDPMessage(w io.Writer, message *protobuf.Message, writerMutex *sync.Mutex, state *ConnState, address string) error {
	if state.IsDial {
		if dialAddr, ok := n.udpDialAddrs.Load(address); ok {
			message.DialAddress = fmt.Sprintf("udp://%s", dialAddr.(string))
		} else {
			log.Errorf("package: failed to load dial address")
		}
	} else {
		message.DialAddress = n.Address
	}

	bytes, err := proto.Marshal(message)
	if err != nil {
		log.Errorf("package: failed to Marshal entire message, err: %f", err.Error())
	}

	buffer := make([]byte, 2)
	binary.BigEndian.PutUint16(buffer, uint16(len(bytes)))
	buffer = append(buffer, bytes...)

	writerMutex.Lock()
	udpConn, ok := state.conn.(*net.UDPConn)
	if !ok {
		log.Errorf("package: failed to write entire message, err: %+v", err)
	}

	if state.IsDial {
		_, err = udpConn.Write(buffer)
	} else {
		index := strings.LastIndex(address, "/")
		resolved, err := net.ResolveUDPAddr("udp", address[index+1:])
		if err != nil {
			return err
		}
		_, err = udpConn.WriteToUDP(buffer, resolved)
	}
	if err != nil {
		log.Errorf("package: failed to write entire message, err: %+v", err)
	}
	writerMutex.Unlock()
	return nil
}

// receiveMessage reads, unmarshals and verifies a message from a net.Conn.
func (n *Network) receiveUDPMessage(conn interface{}) (*protobuf.Message, error) {
	var err error
	buffer := make([]byte, MAX_PACKAGE_SIZE)

	udpConn, _ := conn.(*net.UDPConn)
	_, _, err = udpConn.ReadFromUDP(buffer)
	size := binary.BigEndian.Uint16(buffer[0:2])
	// Deserialize message.
	msg := new(protobuf.Message)
	err = proto.Unmarshal(buffer[2:2+size], msg)
	if err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal message")
	}

	// Check if any of the message headers are invalid or null.
	if msg.Opcode == 0 || msg.Sender == nil || msg.Sender.NetKey == nil || len(msg.Sender.Address) == 0 {
		return nil, errors.New("received an invalid message (either no opcode, no sender, no net key, or no signature) from a peer")
	}

	// Verify signature of message.
	/*	if msg.Signature != nil && !crypto.Verify(
			n.opts.signaturePolicy,
			n.opts.hashPolicy,
			msg.Sender.NetKey,
			SerializeMessage(msg.Sender, msg.Message),
			msg.Signature,
		) {
			return nil, errors.New("received message had an malformed signature")
		}*/

	return msg, nil
}
