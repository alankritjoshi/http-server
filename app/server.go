package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
)

const (
	ok        = "HTTP/1.1 200 OK"
	not_found = "HTTP/1.1 404 NOT FOUND"
)

type request struct {
	headers  map[string]string
	protocol string
}

func buildResponse(protocol string, headers *[]string, content *string) string {
	var builder strings.Builder

	builder.WriteString(protocol + "\r\n")

	if headers != nil {
		builder.WriteString(strings.Join(*headers, "\r\n"))
		builder.WriteString("\r\n")
	}

	builder.WriteString("\r\n")

	if content != nil {
		builder.WriteString(*content + "\r\n")
	}

	return builder.String()
}

type connection struct {
	conn     net.Conn
	reader   *bufio.Reader
	writer   *bufio.Writer
	filesDir string
}

func (c *connection) receive(ctx context.Context) (*request, error) {
	deadline, ok := ctx.Deadline()
	if ok {
		c.conn.SetReadDeadline(deadline)
	}

	headers := make(map[string]string)
	var request request

	protocolProcessed := false
	headersProcessed := false

	for {
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

			line = strings.TrimSuffix(line, "\r\n")

			if !protocolProcessed {
				request.protocol = line
				protocolProcessed = true
			} else if !headersProcessed && len(line) == 0 {
				request.headers = headers
				return &request, nil
			} else if !headersProcessed {
				headerSplit := strings.Split(line, ": ")
				headers[headerSplit[0]] = headerSplit[1]
			} else {
				return nil, fmt.Errorf("invalid request")
			}
		}
	}
}

func (c *connection) send(ctx context.Context, message string) error {
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
			return fmt.Errorf("unable to send message to client")
		}

		return c.writer.Flush()
	}
}

func (c *connection) handle() error {
	ctx := context.Background()

	request, err := c.receive(ctx)
	if err != nil {
		return fmt.Errorf("failed to receive request")
	}

	startLine := request.protocol
	path := strings.Split(startLine, " ")[1]
	pathSplit := strings.Split(path, "/")

	if len(pathSplit) == 2 && pathSplit[1] == "" {
		if err := c.send(ctx, buildResponse(ok, nil, nil)); err != nil {
			return fmt.Errorf("failed to send OK response for root request")
		}

		return nil
	}

	responseType := ok
	contentType := "Content-Type: text/plain"
	var content string
	var contentLength string

	switch pathSplit[1] {
	case "echo":
		content = strings.Join(pathSplit[2:], "/")
	case "user-agent":
		content = request.headers["User-Agent"]
	case "file":
		fileName := strings.Join(pathSplit[2:], "/")

		file, err := os.Open(c.filesDir + "/" + fileName)
		if err != nil {
			if os.IsNotExist(err) {
				responseType = not_found

				if err := c.send(ctx, buildResponse(not_found, nil, nil)); err != nil {
					return fmt.Errorf("failed to send NOT FOUND response for invalid request")
				}

				return nil
			}

			return fmt.Errorf("failed to open file")
		}

		fileInfo, err := file.Stat()
		if err != nil {
			return fmt.Errorf("failed to get file info")
		}

		contentLength = fmt.Sprintf("Content-Length: %d", fileInfo.Size())
		contentType = "Content-Type: application/octet-stream"

	default:
		responseType = not_found
		if err := c.send(ctx, buildResponse(not_found, nil, nil)); err != nil {
			return fmt.Errorf("failed to send NOT FOUND response for invalid request")
		}
	}

	contentLength = fmt.Sprintf("Content-Length: %d", len(content))

	httpMessage := buildResponse(
		responseType,
		&[]string{
			contentType,
			contentLength,
		},
		&content,
	)

	if err := c.send(
		ctx,
		httpMessage,
	); err != nil {
		return fmt.Errorf("failed to send OK response for echo request")
	}

	return nil
}

func (c *connection) close() {
	c.conn.Close()
}

func newConnection(conn net.Conn, filesDir string) (*connection, error) {
	return &connection{
		conn:     conn,
		reader:   bufio.NewReader(conn),
		writer:   bufio.NewWriter(conn),
		filesDir: filesDir,
	}, nil
}

func main() {
	dirFlag := flag.String("directory", ".", "directory to serve files from")

	flag.Parse()

	if err := os.MkdirAll(*dirFlag, 0755); err != nil {
		fmt.Println("Failed to create directory")
		os.Exit(1)
	}

	l, err := net.Listen("tcp", "localhost:4221")
	if err != nil {
		fmt.Println("Failed to bind to port 4221")
		os.Exit(1)
	}

	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Failed to accept client connection")
			os.Exit(1)
		}

		c, err := newConnection(conn, *dirFlag)
		if err != nil {
			fmt.Println("Failed to create new connection")
			os.Exit(1)
		}

		go func() {
			defer c.close()

			err := c.handle()
			if err != nil {
				fmt.Printf("Failed to handle connection: %v\n", err)
				return
			}
		}()
	}
}
