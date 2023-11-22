package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"
)

func main() {
	l, err := net.Listen("tcp", "localhost:4221")
	if err != nil {
		fmt.Println("Failed to bind to port 4221")
		os.Exit(1)
	}

	conn, err := l.Accept()
	if err != nil {
		fmt.Println("Error accepting connection: %w", err.Error())
		os.Exit(1)
	}

	defer conn.Close()

	buffer := make([]byte, 1024)
	var requestBuilder strings.Builder

	for {
		n, err := conn.Read(buffer)
		if err != nil {
			if err == io.EOF {
				break
			}
			fmt.Println("Error reading request:", err.Error())
			return
		}

		requestBuilder.Write(buffer[:n])
		if strings.Contains(requestBuilder.String(), "\r\n\r\n") {
			break
		}
	}

	response := "HTTP/1.1 200 OK\r\n\r\n"

	_, err = conn.Write([]byte(response))
	if err != nil {
		fmt.Println("Error writing response: %w", err.Error())
		os.Exit(1)
	}
}
