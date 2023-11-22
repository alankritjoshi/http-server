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

func HTTPString(line string) string {
	return fmt.Sprintf("%s\r\n", line)
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
		if err := client.send(ctx, []string{HTTPString(OK)}); err != nil {
			fmt.Println("Failed to send OK response for root request")
			os.Exit(1)
		}
	}

	if len(pathSplit) < 2 || pathSplit[1] != "echo" {
		if err := client.send(ctx, []string{HTTPString(NOT_FOUND)}); err != nil {
			fmt.Println("Failed to send NOT FOUND response for non-echo request")
			os.Exit(1)
		}
	}

	responseType := HTTPString(OK)
	contentType := HTTPString("Content-Type: text/plain")
	content := HTTPString(strings.Join(pathSplit[1:], "/"))
	contentLength := HTTPString(fmt.Sprintf("Content-Length: %d", len(content)))

	if err := client.send(ctx, []string{
		responseType,
		contentType,
		HTTPString(""),
		contentLength,
		content,
	}); err != nil {
		fmt.Println("Failed to send OK response for echo request")
		os.Exit(1)
	}
}
