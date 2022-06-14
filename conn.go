package main

import (
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/kevwan/tproxy/display"
	"github.com/kevwan/tproxy/protocol"
)

const (
	serverSide      = "SERVER"
	clientSide      = "CLIENT"
	useOfClosedConn = "use of closed network connection"
)

type PairedConnection struct {
	id      int
	cliConn net.Conn
	svrConn net.Conn
	once    sync.Once
}

func NewPairedConnection(id int, cliConn net.Conn) *PairedConnection {
	return &PairedConnection{
		id:      id,
		cliConn: cliConn,
	}
}

func (c *PairedConnection) handleClientMessage() {
	r, w := io.Pipe()
	tee := io.MultiWriter(c.svrConn, w)

	go protocol.NewDumper(r, clientSide, c.id, settings.Silent, protocol.CreateInterop(settings.Protocol)).Dump()

	_, e := io.Copy(tee, c.cliConn)
	if e != nil && e != io.EOF {
		fmt.Printf("handleClientMessage: io.Copy error: %v", e)
	}
}

func (c *PairedConnection) handleServerMessage() {
	r, w := io.Pipe()
	tee := io.MultiWriter(c.cliConn, w)
	go protocol.NewDumper(r, serverSide, c.id, settings.Silent, protocol.CreateInterop(settings.Protocol)).Dump()
	_, e := io.Copy(tee, c.svrConn)
	if e != nil && e != io.EOF {
		netOpError, ok := e.(*net.OpError)
		if ok && netOpError.Err.Error() != useOfClosedConn {
			fmt.Printf("handleServerMessage: io.Copy error: %v", e)
		}
	}

	c.stop()
}

func (c *PairedConnection) process() {
	conn, err := net.Dial("tcp", settings.RemoteHost)
	if err != nil {
		display.PrintfWithTime("[x][%d] Couldn't connect to server: %v\n", c.id, err)
		return
	}

	display.PrintfWithTime("[*][%d] Connected to server: %s\n", c.id, conn.RemoteAddr())

	c.svrConn = conn
	go c.handleServerMessage()

	c.handleClientMessage()
	c.stop()
}

func (c *PairedConnection) stop() {
	c.once.Do(func() {
		if c.cliConn != nil {
			display.PrintfWithTime("[*][%d] Client connection closed\n", c.id)
			c.cliConn.Close()
		}
		if c.svrConn != nil {
			display.PrintfWithTime("[*][%d] Server connection closed\n", c.id)
			c.svrConn.Close()
		}
	})
}

func startListener() error {
	conn, err := net.Listen("tcp", fmt.Sprint(settings.LocalHost, ":", settings.LocalPort))
	if err != nil {
		return fmt.Errorf("failed to start listener: %w", err)
	}

	display.PrintlnWithTime("[*] Listening...")
	defer conn.Close()

	var connIndex int
	for {
		cliConn, err := conn.Accept()
		if err != nil {
			return fmt.Errorf("server: accept: %w", err)
		}

		connIndex++
		display.PrintfWithTime("[*][%d] Accepted from: %s\n", connIndex, cliConn.RemoteAddr())

		pconn := NewPairedConnection(connIndex, cliConn)
		go pconn.process()
	}
}