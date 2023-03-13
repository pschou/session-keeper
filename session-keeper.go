// Copyright 2019 github.com/pschou/session-keeper
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	listen  = flag.String("listen", ":2222", "Where to listen to incoming connections (example 1.2.3.4:8080)")
	target  = flag.String("target", "localhost:2020", "Remote SSHProxy to connect to")
	verbose = flag.Bool("verbose", false, "Turn on verbosity")
)

func main() {
	flag.Parse()

	postSetup()

	// Listen for incoming connections.
	l, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Println("Error listening:", err.Error())
		os.Exit(1)
	}
	// Close the listener when the application closes.
	defer l.Close()
	fmt.Println("Listening on " + *listen)
	for {
		// Listen for an incoming connection.
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Error accepting: ", err.Error())
			os.Exit(1)
		}
		// Handle connections in a new goroutine.
		go handleRequest(conn)
	}
}

func handleRequest(conn net.Conn) {
	if *verbose {
		log.Println("Incoming connection", conn.RemoteAddr())
	}

	var dstConn net.Conn
	var remoteClose bool
	hdr := ConnHeader{UUID: uuid.New(), Offset: -1}

	// Make sure all the time we have sent (or tried to send) an EOF signal.
	defer func() {
		conn.Close()
		if !remoteClose && dstConn != nil {
			if *verbose {
				log.Println("sending EOF signal")
			}
			if dstConn, err := net.Dial("tcp", *target); err == nil {
				EOFhdr := ConnHeader{UUID: hdr.UUID, Offset: -3}
				// Write out an EOF packet to a new connection to terminate the stream
				binary.Write(dstConn, binary.BigEndian, EOFhdr)
				// kind of doesn't matter if the error happens, as, well, we tried!
				dstConn.Close()
			}
		}
	}()

	// Parse the initial proxy connection
	var hostport string
	for i := 0; i < 100; i++ { // parse first 100 lines and give up
		line, err := ReadLine(conn, '\n')
		if err != nil {
			return
		}
		if strings.HasPrefix(line, "CONNECT ") && strings.HasSuffix(line, " HTTP/1.1") {
			// when the CONNECT line is found, consume it
			hostport = line[8 : len(line)-9]
			_, _, err = net.SplitHostPort(hostport)
			if err != nil {
				// Invalid host:port, give up early
				return
			}
		} else if line == "" {
			break
		}
	}
	if hostport == "" {
		// no CONNECT line was found, or it was empty
		return
	}
	if *verbose {
		log.Println("Got CONNECT to", hostport)
	}

	// Go ahead and start reading into a buffer from the local connection
	var buf bytes.Buffer
	var bufMutex sync.Mutex
	var bufOffset int64
	var closeLocal bool
	var C = make(chan bool, 3)

	// Do the work of the read from local
	go func() {
		readBuf := make([]byte, 10002)
		for !closeLocal {
			n, err := conn.Read(readBuf[2:])
			if err != nil {
				closeLocal = true
			}
			readBuf[0], readBuf[1] = byte(n>>8), byte(n&0xff)
			bufMutex.Lock()
			buf.Write(readBuf[:n+2])
			bufMutex.Unlock()
			if len(C) == 0 {
				C <- true
			}
		}
		conn.Close()
		if dstConn != nil {
			dstConn.Close()
		}
		close(C)
	}()

	// Establish an outgoing connection with a retry counter
	var err error
	for i := 0; i < 100 && err == nil && !closeLocal; i++ { // 100 retries
		if *verbose && hdr.Offset >= 0 {
			log.Println("reconnecting  hoff:", hdr.Offset)
		}

		// Thread to handle the outgoing connection
		err = func() error {
			if *verbose {
				log.Println("Dialing ", *target)
			}
			if dstConn, err = net.Dial("tcp", *target); err != nil {
				if hdr.Offset == -1 {
					return net.ErrClosed // On first connection, give up early
				}
				time.Sleep(time.Second * 3)
				return nil // Cannot connect to endpoint, go back and loop
			}

			// Ensure the outgoing connection is closed
			defer dstConn.Close()

			if *verbose {
				log.Println("Writing header", hdr.UUID.String())
			}
			if err = binary.Write(dstConn, binary.BigEndian, hdr); err != nil {
				return fmt.Errorf("Could not write to connection: %s", err)
			}

			if hdr.Offset == -1 {
				// This is a new connection
				fmt.Fprintf(dstConn, "%s\n", hostport) // write out the connect header
			}

			// Now read back the remote header
			var rcvHdr ConnHeader
			if *verbose {
				log.Println("Reading header", hdr.UUID.String())
			}

			go func() {
				// kill function for fast reconnects
				time.Sleep(3 * time.Second)
				if !bytes.Equal(rcvHdr.UUID[:], hdr.UUID[:]) {
					dstConn.Close()
				}
			}()

			err = binary.Read(dstConn, binary.BigEndian, &rcvHdr)
			if err != nil {
				time.Sleep(time.Second * 3)
				return nil
			}

			if *verbose {
				log.Println("Got header", rcvHdr)
			}

			// Give up early, bad server reply!
			if !bytes.Equal(rcvHdr.UUID[:], hdr.UUID[:]) {
				return errors.New("UUID does not match")
			}

			// Close the connection when there is a remote EOF signal
			if rcvHdr.Offset == -4 {
				remoteClose = true
				conn.Close()
				return io.EOF
			}

			// Compare that both are at the start
			if hdr.Offset == -1 {
				if rcvHdr.Offset != -2 {
					return errors.New("New session not established")
				}
				if *verbose {
					log.Println("Session established")
				}
				hdr.Offset, rcvHdr.Offset = 0, 0
				conn.Write([]byte("HTTP/1.0 200 Connection Established\r\n" +
					"Connection: close\r\n" +
					"\r\n"))
			}

			if rcvHdr.Offset < bufOffset || rcvHdr.Offset > bufOffset+int64(buf.Len()) {
				return errors.New("Buffer failed to maintain state")
			}

			// We're in a good state
			i = 0 // Restart the counter as we connected and established a session

			// Advance the local buffer if we need to
			for rcvHdr.Offset > bufOffset {
				bufMutex.Lock()
				sz := buf.Next(2)
				tosend := buf.Next(int(sz[0])<<8 + int(sz[1]))
				bufOffset += int64(len(tosend))
				bufMutex.Unlock()
				//buf.Next(int(bufOffset - rcvHdr.Offset)) // throw away what we don't need
				//bufOffset = rcvHdr.Offset
			}

			// Do the work of the read from remote and printing locally
			var localErr error
			var close bool
			go func() { // Create thread for reading with close
				defer func() {
					close = true
					C <- true
				}()

				rcvBuf := make([]byte, 1<<10)
				for !close && !closeLocal { // infinite loop reading from DST
					n, err := dstConn.Read(rcvBuf)
					if close || err != nil {
						return
					}
					tosend := rcvBuf[:n]
					for len(tosend) > 0 {
						if *verbose {
							fmt.Printf("fromDST %q  hoff: %d\n", tosend, hdr.Offset)
						}
						wn, writeErr := conn.Write(tosend)
						hdr.Offset += int64(wn)
						if writeErr != nil {
							localErr = writeErr
							closeLocal = true
							return
						}
						tosend = tosend[wn:]
					}
				}
			}()

			// Read from the buffer and write to remote
			timer := time.NewTicker(3 * time.Second)
			for !close {
				select {
				case <-timer.C:
				case <-C:
				}
				if buf.Len() == 0 { // simulate activity, empty traffic
					_, writeErr := dstConn.Write([]byte{})
					if writeErr != nil {
						close = true
					}
				}
				for buf.Len() >= 2 {
					bufMutex.Lock()
					b := buf.Bytes()
					sz := (int(b[0]) << 8) + int(b[1])
					tosend := b[2 : 2+sz]
					if *verbose {
						fmt.Printf("toDST %q  off: %d buf: %d\n", tosend, bufOffset, buf.Len())
					}
					wn, writeErr := dstConn.Write(tosend)
					if writeErr != nil {
						close = true
					} else {
						buf.Next(wn + 2)
						bufOffset += int64(wn)
					}
					bufMutex.Unlock()
				}
			}
			dstConn.Close()
			timer.Stop()
			return localErr
		}()
		if err != nil && *verbose {
			log.Println("error in session", err)
		}
	}
}
