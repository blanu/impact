package main

import (
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"internal/request"
	"internal/response"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
)

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

	connectionToResource := make(chan request.Request)

	// If we can't launch the resource, we must give up.
	resourceError := startResource(*path, connectionToResource)
	if resourceError != nil {
		print(resourceError)
		os.Exit(12)
	}

	// If we can't listen, we must give up.
	listener, listenError := net.Listen("tcp", "0.0.0.0:"+strconv.Itoa(*port))
	if listenError != nil {
		os.Exit(10)
	}

	for {
		// The purpose of this program is to give shared access for a resource to multiple connections.
		connection, acceptError := listener.Accept()

		// If we failed to accept then the listen is broken. We could continue to serve existing connections, but since
		// this should never happen, let's give up instead.
		if acceptError != nil {
			os.Exit(11)
		}

		// Access to the shared resources is concurrent from all connections
		go handleConnection(connection, connectionToResource)
	}
}

// Attempt to start the resource as a separate process connected to us through stdin/stdout
func startResource(path string, connectionToResource chan request.Request) error {
	ctx, cancel := context.WithCancel(context.Background())
	resource := exec.CommandContext(ctx, path)
	resourceInput, inputError := resource.StdinPipe()
	if inputError != nil {
		return inputError
	}
	resourceOutput, outputError := resource.StdoutPipe()
	if outputError != nil {
		return outputError
	}

	startError := resource.Start()
	if startError != nil {
		cancel()
		return errors.New("resource could not be started")
	}

	// We start processing access to the shared resource here because we have access to the stdin/stdout streams.
	go pumpConnectionToResource(connectionToResource, resourceInput, resourceOutput, cancel)

	return nil
}

// The pumpConnectionToResource connects the shared resources with the connections that are accessing it.
// The represents the resource's perspective on the interaction.
func pumpConnectionToResource(connectionToResource chan request.Request, resourceInput io.WriteCloser, resourceOutput io.ReadCloser, cancel context.CancelFunc) {
	// Locking for concurrent connections is handled by a request channel.
	// For each incoming request, we process it one at a time, atomically.
	for request := range connectionToResource {
		writeError := fullWriteFile(resourceInput, request.Payload)
		if writeError != nil {
			request.ReplyChannel <- response.Response{nil, true}
		}

		reply, readError := readMessageFromStream(resourceOutput)
		if readError != nil {
			fmt.Println(readError)
			os.Exit(21)
		}

		request.ReplyChannel <- response.Response{reply, false}
	}

	cancel()
}

// The connection handler represents the connection's perspective on the interaction with the shared resource.
func handleConnection(connection net.Conn, connectionToResource chan request.Request) {
	resourceToConnection := make(chan response.Response)

	for {
		message, readError := readMessageFromConn(connection)
		if readError != nil {
			fmt.Println(readError)
			os.Exit(22)
		}

		request := request.Request{message, resourceToConnection}
		connectionToResource <- request

		response := <-resourceToConnection

		if response.Payload != nil {
			connection.Write(response.Payload)
		}

		if response.Close {
			closeError := connection.Close()
			if closeError != nil {
				fmt.Println(closeError)
			}

			return
		}
	}
}

// Messages are in a format consisting of a payload prefixed by a varint-encoded length.
// Please note that there are multiple known formats for varint encoding.
// The format used here is not the one from the Go standard library.
// This function reads messages from the resource's stdout
func readMessageFromStream(stream io.ReadCloser) ([]byte, error) {
	prefix, prefixReadError := fullReadFile(stream, 1)
	if prefixReadError != nil {
		return nil, prefixReadError
	}
	varintCount := int(prefix[0])

	compressedBuffer, compressedReadError := fullReadFile(stream, varintCount)
	if compressedReadError != nil {
		return nil, compressedReadError
	}

	uncompressedBuffer, unpackError := unpackVarintData(compressedBuffer)
	if unpackError != nil {
		return nil, unpackError
	}

	payloadCount := dataToInt(uncompressedBuffer)
	payload, payloadReadError := fullReadFile(stream, payloadCount)
	if payloadReadError != nil {
		return nil, payloadReadError
	}

	completeMessage := make([]byte, 0)
	completeMessage = append(completeMessage, prefix...)
	completeMessage = append(completeMessage, compressedBuffer...)
	completeMessage = append(completeMessage, payload...)

	return completeMessage, nil
}

// Messages are in a format consisting of a payload prefixed by a varint-encoded length.
// Please note that there are multiple known formats for varint encoding.
// The format used here is not the one from the Go standard library.
// This function reads messages from the connection.
func readMessageFromConn(conn net.Conn) ([]byte, error) {
	prefix, prefixReadError := fullRead(conn, 1)
	if prefixReadError != nil {
		return nil, prefixReadError
	}
	varintCount := int(prefix[0])

	compressedBuffer, compressedReadError := fullRead(conn, varintCount)
	if compressedReadError != nil {
		return nil, compressedReadError
	}

	uncompressedBuffer, unpackError := unpackVarintData(compressedBuffer)
	if unpackError != nil {
		return nil, unpackError
	}

	payloadCount := dataToInt(uncompressedBuffer)
	payload, payloadReadError := fullRead(conn, payloadCount)
	if payloadReadError != nil {
		return nil, payloadReadError
	}

	completeMessage := make([]byte, 0)
	completeMessage = append(completeMessage, prefix...)
	completeMessage = append(completeMessage, compressedBuffer...)
	completeMessage = append(completeMessage, payload...)

	return completeMessage, nil
}

// We need this to ensure that there are no short reads from the file.
func fullReadFile(conn io.ReadCloser, size int) ([]byte, error) {
	buffer := make([]byte, size)

	totalRead := 0
	for totalRead < size {
		numRead, readError := conn.Read(buffer)
		if readError != nil {
			return nil, readError
		}

		totalRead += numRead
	}

	return buffer, nil
}

// We need this to ensure that there are no short reads from the connection.
func fullRead(conn net.Conn, size int) ([]byte, error) {
	buffer := make([]byte, size)

	totalRead := 0
	for totalRead < size {
		numRead, readError := conn.Read(buffer)
		if readError != nil {
			return nil, readError
		}

		totalRead += numRead
	}

	return buffer, nil
}

// We need this to ensure that there are no short writes to the file.
func fullWriteFile(conn io.WriteCloser, message []byte) error {
	totalWritten := 0
	for totalWritten < len(message) {
		numWritten, writeError := conn.Write(message[totalWritten:])
		if writeError != nil {
			return writeError
		}

		totalWritten += numWritten
	}

	return nil
}

// We need this to ensure that there are no short writes to the connection.
func fullWrite(conn net.Conn, message []byte) error {
	totalWritten := 0
	for totalWritten < len(message) {
		numWritten, writeError := conn.Write(message[totalWritten:])
		if writeError != nil {
			return writeError
		}

		totalWritten += numWritten
	}

	return nil
}

// This is one phase of the varint decoder. It takes a compressed length (1-8 bytes) and makes an 8-byte length.
func unpackVarintData(buffer []byte) ([]byte, error) {
	if buffer == nil {
		return nil, errors.New("buffer was nil")
	}

	if len(buffer) == 0 {
		return nil, errors.New("buffer was empty")
	}

	count := len(buffer)
	gap := 8 - count

	uint64Bytes := make([]byte, 8)
	copy(uint64Bytes[gap:], buffer[:count])

	return uint64Bytes, nil
}

// This is one phase of the varint decoder. It takes an 8-byte length and returns an int.
func dataToInt(buffer []byte) int {
	uintValue := binary.BigEndian.Uint64(buffer)
	intValue := int(uintValue)
	return intValue
}
