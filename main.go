package main

import (
	"flag"
	"github.com/blanu/radiowave"
	"internal/message"
	"internal/request"
	"os"
	"strconv"
)

// The purpose of impact is to provided multi-user serialized access to a resource.
func main() {
	port := flag.Int("port", 1111, "port on which to listen")
	if port == nil {
		print("port required")
		os.Exit(3)
	}

	path := flag.String("path", "", "path for shared resource executable")
	if path == nil || *path == "" {
		print("No path to resource")
		os.Exit(9)
	}

	factory := message.NewImpactMessageFactory()

	// If we can't launch the resource, we must give up.
	process, resourceError := radiowave.Exec(factory, *path)
	if resourceError != nil {
		print(resourceError)
		os.Exit(12)
	}

	// If we can't listen, we must give up.
	listener, listenError := radiowave.Listen(factory, "0.0.0.0:"+strconv.Itoa(*port))
	if listenError != nil {
		os.Exit(10)
	}

	// There is only one process handler coroutine
	funnel := make(chan request.Request)
	go handleProcess(*process, funnel)

	for {
		// The purpose of this program is to give shared access for a resource to multiple connections.
		connection, acceptError := listener.Accept()

		// If we failed to accept then the listen is broken. We could continue to serve existing connections, but since
		// this should never happen, let's give up instead.
		if acceptError != nil {
			os.Exit(11)
		}

		// Access to the shared resources is concurrent from all connections
		// There is one connection handler coroutine for each connection.
		go handleConnection(*connection, funnel)
	}
}

// The connection handler represents the connection's perspective on the interaction with the shared resource.
func handleConnection(connection radiowave.Conn, funnel chan request.Request) {
	// We're in charge on one connection.

	// This is our dedicated response channel just for this connection.
	responseChannel := make(chan radiowave.Message)

	// Process each message from the connection.
	for wave := range connection.OutputChannel {
		// Package this request up in a Request callback.
		// The callback includes our dedicated response channel.
		request := request.Request{wave, responseChannel}

		// All requests go into the funnel. There is just one funnel because there is just one process.
		funnel <- request

		// Now we wait for a response on our dedicated response channel.
		response := <-responseChannel

		// Send the response back to the connection.
		connection.InputChannel <- response
	}
}

func handleProcess(process radiowave.Process, funnel chan request.Request) {
	// We only have one process.
	// Requests will come in from multiple connections.
	// We serialize them to provide multi-user access to the resource.
	for request := range funnel {
		// We have a message from the funnel.
		// Send it to the process.
		process.InputChannel <- request.Message

		// Get the reply from the process.
		reply := <-process.OutputChannel

		// Send the reply back on the dedicated reply channel.
		request.ReplyChannel <- reply
	}

	// No more messages from the process means that it has terminated.
	os.Exit(40)
}
