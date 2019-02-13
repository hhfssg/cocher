package main

import _ "net/http/pprof"

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/cocher/crypto/ed25519"
	"github.com/cocher/examples/local_benchmark/messages"
	"github.com/cocher/network"
	"github.com/cocher/types/opcode"
)

type BenchmarkComponent struct {
	*network.Component
	counter int
}

func (state *BenchmarkComponent) Receive(ctx *network.ComponentContext) error {
	switch ctx.Message().(type) {
	case *messages.BasicMessage:
		state.counter++
	}

	return nil
}

var profile = flag.String("profile", "", "write cpu profile to file")
var Address = map[string]string{
	"tcp": "tcp://localhost:3001",
	"udp": "udp://localhost:3001",
	"kcp": "kcp://localhost:3001",
}

func main() {
	flag.Set("logtostderr", "true")

	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()

	protocolFlag := flag.String("protocol", "tcp", "protocol to use (kcp/tcp/udp)")
	flag.Parse()
	protocol := *protocolFlag
	runtime.GOMAXPROCS(runtime.NumCPU())
	opcode.RegisterMessageType(opcode.Opcode(1000), &messages.BasicMessage{})

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	go func() {
		<-c
		pprof.StopCPUProfile()
		os.Exit(0)
	}()

	if *profile != "" {
		f, err := os.Create(*profile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
	}

	builder := network.NewBuilder()
	builder.SetAddress(Address[protocol])
	builder.SetKeys(ed25519.RandomKeyPair())

	state := new(BenchmarkComponent)
	builder.AddComponent(state)

	net, err := builder.Build()
	if err != nil {
		panic(err)
	}

	go net.Listen()

	fmt.Println("Waiting for sender on ", Address[protocol])

	// Run loop every 1 second.
	for range time.Tick(1 * time.Second) {
		fmt.Printf("Got %d messages.\n", state.counter)

		state.counter = 0
	}
}
