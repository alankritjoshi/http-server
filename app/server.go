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
	OK        = "HTTP/1.1 200 OK\r\n\r\n"
	NOT_FOUND = "HTTP/1.1 404 NOT FOUND\r\n\r\n"
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

func (c *Client) send(ctx context.Context, lines []string) error {
	deadline, ok := ctx.Deadline()
	if ok {
		c.conn.SetWriteDeadline(deadline)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		for _, line := range lines {
			_, err := c.writer.WriteString(line)
			if err != nil {
				return err
			}
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

	response := OK

	if path != "/" {
		response = NOT_FOUND
	}

	contentType := "Content-Type: text/html\r\n"
	contentLength := "Content-Length: 11\r\n\r\n"
	content := "Hello World"

	client.send(ctx, []string{
		response,
		contentType,
		contentLength,
		content,
	})
}
