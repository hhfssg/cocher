package test

import (
	"github.com/cocher/internal/test/protobuf"
	"github.com/cocher/network"
)

var (
	mailboxComponentID = (*MailBoxComponent)(nil)
)

// MailBoxComponent buffers all messages into a mailbox for test validation.
type MailBoxComponent struct {
	*network.Component
	RecvMailbox chan *protobuf.TestMessage
	SendMailbox chan *protobuf.TestMessage
}

// Startup creates a mailbox channel
func (state *MailBoxComponent) Startup(net *network.Network) {
	state.RecvMailbox = make(chan *protobuf.TestMessage)
	state.SendMailbox = make(chan *protobuf.TestMessage)
}

// Send puts a sent message into the SendMailbox channel
func (state *MailBoxComponent) Send(ctx *network.ComponentContext) error {
	switch msg := ctx.Message().(type) {
	case *protobuf.TestMessage:
		state.SendMailbox <- msg
	}
	return nil
}

// Receive puts a received message into the RecvMailbox channel
func (state *MailBoxComponent) Receive(ctx *network.ComponentContext) error {
	switch msg := ctx.Message().(type) {
	case *protobuf.TestMessage:
		state.RecvMailbox <- msg
	}
	return nil
}
