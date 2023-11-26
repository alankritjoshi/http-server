package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
)

const (
	ok        = "HTTP/1.1 200 OK"
	created   = "HTTP/1.1 201 CREATED"
	not_found = "HTTP/1.1 404 NOT FOUND"
)

type request struct {
	headers  map[string]string
	protocol string
	content  string
}

func buildResponse(protocol string, headers *[]string, content string) []byte {
	var builder strings.Builder

	builder.WriteString(protocol + "\r\n")

	if headers != nil {
		builder.WriteString(strings.Join(*headers, "\r\n"))
		builder.WriteString("\r\n")
	}

	builder.WriteString("\r\n")

	if len(content) != 0 {
		builder.WriteString(content + "\r\n")
	}

	return []byte(builder.String())
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

	contentExpectedLength := 0
	contentProcessedLength := 0

	var contentBuilder strings.Builder

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
			} else if !headersProcessed {
				if len(line) == 0 {
					request.headers = headers
					headersProcessed = true
					if _, ok := request.headers["Content-Length"]; !ok {
						return &request, nil
					}
					contentExpectedLength, err = strconv.Atoi(request.headers["Content-Length"])
					if err != nil {
						return nil, fmt.Errorf("invalid content length")
					}
				} else {
					headerSplit := strings.Split(line, ": ")
					headers[headerSplit[0]] = headerSplit[1]
				}
			} else {
				contentBuilder.WriteString(line)
				contentProcessedLength += len(line)
				if contentProcessedLength == contentExpectedLength {
					request.content = contentBuilder.String()
					return &request, nil
				}
			}
		}
	}
}

func (c *connection) send(ctx context.Context, message []byte) error {
	deadline, ok := ctx.Deadline()
	if ok {
		c.conn.SetWriteDeadline(deadline)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		_, err := c.writer.Write(message)
		if err != nil {
			return fmt.Errorf("unable to send message to client")
		}

		return c.writer.Flush()
	}
}

func (c *connection) handleGet(ctx context.Context, request *request) error {
	startLine := request.protocol
	path := strings.Split(startLine, " ")[1]
	pathSplit := strings.Split(path, "/")

	if len(pathSplit) == 2 && pathSplit[1] == "" {
		if err := c.send(ctx, buildResponse(ok, nil, "")); err != nil {
			return fmt.Errorf("failed to send OK response for root request")
		}

		return nil
	}

	responseType := ok
	var (
		headers []string

		stringContent string

		fileContent []byte
	)

	switch pathSplit[1] {
	case "echo":
		stringContent = strings.Join(pathSplit[2:], "/")
		contentType := "Content-Type: text/plain"
		contentLength := fmt.Sprintf("Content-Length: %d", len(stringContent))
		headers = []string{
			contentType,
			contentLength,
		}
	case "user-agent":
		stringContent = request.headers["User-Agent"]
		contentType := "Content-Type: text/plain"
		contentLength := fmt.Sprintf("Content-Length: %d", len(stringContent))
		headers = []string{
			contentType,
			contentLength,
		}
	case "files":
		fileName := c.filesDir + "/" + strings.Join(pathSplit[2:], "/")

		fileInfo, err := os.Stat(fileName)
		if (err != nil && os.IsNotExist(err)) || fileInfo.IsDir() {
			responseType = not_found
			break
		}
		if err != nil {
			return fmt.Errorf("failed to get file info for file name %s", fileName)
		}

		file, err := os.Open(fileName)
		if err != nil {
			return fmt.Errorf("failed to open file: %w", err)
		}

		defer file.Close()

		contentType := "Content-Type: application/octet-stream"
		contentLength := fmt.Sprintf("Content-Length: %d", fileInfo.Size())

		headers = []string{
			contentType,
			contentLength,
		}

		reader := bufio.NewReader(file)

		fileContent, err = io.ReadAll(reader)
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}
	default:
		responseType = not_found
		if err := c.send(ctx, buildResponse(not_found, nil, "")); err != nil {
			return fmt.Errorf("failed to send NOT FOUND response for invalid request: %w", err)
		}
	}

	httpMessage := buildResponse(
		responseType,
		&headers,
		stringContent,
	)

	if err := c.send(
		ctx,
		httpMessage,
	); err != nil {
		return fmt.Errorf("failed to send http response %v: %w", httpMessage, err)
	}

	if fileContent != nil {
		if err := c.send(
			ctx,
			fileContent,
		); err != nil {
			return fmt.Errorf("failed to send file content: %w", err)
		}
	}

	return nil
}

func (c *connection) handlePost(ctx context.Context, request *request) error {
	startLine := request.protocol
	path := strings.Split(startLine, " ")[1]
	pathSplit := strings.Split(path, "/")

	if len(pathSplit) < 4 || pathSplit[1] != "files" || pathSplit[2] != c.filesDir {
		if err := c.send(ctx, buildResponse(not_found, nil, "")); err != nil {
			return fmt.Errorf("failed to send NOT FOUND response for invalid request")
		}

		return nil
	}

	fileName := strings.Join(pathSplit[2:], "/")

	file, err := os.Create(fileName)
	if err != nil {
		return fmt.Errorf("failed to create file at %s: %w", fileName, err)
	}

	defer file.Close()

	file.WriteString(request.content)

	if err := c.send(
		ctx,
		buildResponse(
			created,
			nil,
			"",
		),
	); err != nil {
		return fmt.Errorf("failed to send OK response for POST request")
	}

	return nil
}

func (c *connection) handle() error {
	ctx := context.Background()

	request, err := c.receive(ctx)
	if err != nil {
		return fmt.Errorf("failed to receive request")
	}

	requestVerb := strings.Split(request.protocol, " ")[0]

	switch requestVerb {
	case "GET":
		if err := c.handleGet(ctx, request); err != nil {
			return fmt.Errorf("failed to handle GET request: %w", err)
		}
	case "POST":
		if err := c.handlePost(ctx, request); err != nil {
			return fmt.Errorf("failed to handle POST request: %w", err)
		}
	default:
		return fmt.Errorf("invalid/unsupported request verb: %s", requestVerb)
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
