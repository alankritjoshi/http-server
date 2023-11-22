package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

const (
	OK        = "HTTP/1.1 200 OK"
	NOT_FOUND = "HTTP/1.1 404 NOT FOUND"
)

type Client struct {
	conn     net.Conn
	reader   *bufio.Reader
	writer   *bufio.Writer
	clientID string
}

func (c *Client) receive(ctx context.Context) ([]string, error) {
	deadline, ok := ctx.Deadline()
	if ok {
		c.conn.SetReadDeadline(deadline)
	}

	var lines []string
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		line, err := c.reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		lines = append(lines, strings.TrimRight(line, "\r\n"))
	}
	return lines, nil
}

func (c *Client) send(ctx context.Context, message string) error {
	deadline, ok := ctx.Deadline()
	if ok {
		c.conn.SetWriteDeadline(deadline)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		_, err := c.writer.WriteString(message)
		if err != nil {
			return fmt.Errorf("unable to send message to %s", c.clientID)
		}

		return c.writer.Flush()
	}
}

func (c *Client) close() {
	c.conn.Close()
}

func NewClient(l net.Listener, clientID string) (*Client, error) {
	conn, err := l.Accept()
	if err != nil {
		return nil, err
	}

	return &Client{
		conn:     conn,
		reader:   bufio.NewReader(conn),
		writer:   bufio.NewWriter(conn),
		clientID: clientID,
	}, nil
}

func HTTPMessage(protocol string, headers *[]string, content *string) string {
	var builder strings.Builder

	builder.WriteString(protocol + "\r\n")

	if headers != nil {
		builder.WriteString(strings.Join(*headers, "\r\n"))
	}

	builder.WriteString("\r\n")

	if content != nil {
		builder.WriteString(*content + "\r\n")
	}

	return builder.String()
}

func main() {
	l, err := net.Listen("tcp", "localhost:4221")
	if err != nil {
		fmt.Println("Failed to bind to port 4221")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	defer cancel()

	client, err := NewClient(l, "client-1")
	if err != nil {
		fmt.Println("Failed to accept client connection")
		os.Exit(1)
	}

	defer client.close()

	request, err := client.receive(ctx)
	if err != nil {
		fmt.Println("Failed to receive request")
		os.Exit(1)
	}

	startLine := request[0]
	path := strings.Split(startLine, " ")[1]
	pathSplit := strings.Split(path, "/")

	if len(pathSplit) == 2 && pathSplit[1] == "" {
		if err := client.send(ctx, HTTPMessage(OK, nil, nil)); err != nil {
			fmt.Println("Failed to send OK response for root request")
			os.Exit(1)
		}
	}

	if len(pathSplit) < 2 || pathSplit[1] != "echo" {
		if err := client.send(ctx, HTTPMessage(NOT_FOUND, nil, nil)); err != nil {
			fmt.Println("Failed to send NOT FOUND response for non-echo request")
			os.Exit(1)
		}
	}

	responseType := OK
	contentType := "Content-Type: text/plain"
	content := strings.Join(pathSplit[2:], "/")
	contentLength := fmt.Sprintf("Content-Length: %d", len(content))

	if err := client.send(
		ctx,
		HTTPMessage(
			responseType,
			&[]string{
				contentType,
				contentLength,
			},
			&content,
		),
	); err != nil {
		fmt.Println("Failed to send OK response for echo request")
		os.Exit(1)
	}
}
