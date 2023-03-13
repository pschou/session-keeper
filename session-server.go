package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	listen = flag.String("listen", ":2020",
		"Where to listen to incoming connections (example 1.2.3.4:8080)")
	verbose      = flag.Bool("verbose", false, "Turn on verbosity")
	portRange    = flag.String("allowed", "1-65535", "Allowed destination ports")
	allowedPorts map[int]struct{}
)

func main() {
	flag.Parse()

	allowedPorts = hypenRange(*portRange)

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

var (
	connMap   = make(map[uuid.UUID]*session)
	connMutex sync.Mutex
)

type session struct {
	hdr *ConnHeader

	buf         bytes.Buffer
	bufOffset   int64
	bufMutex    sync.Mutex
	conn, trans net.Conn
	C           chan bool
	closeLocal  bool

	seen  time.Time
	mutex sync.Mutex
}

func handleRequest(conn net.Conn) {
	defer conn.Close()
	if *verbose {
		log.Println("incoming from", conn.RemoteAddr())
	}

	var rcvHdr ConnHeader
	binary.Read(conn, binary.BigEndian, &rcvHdr)
	if bytes.Equal(rcvHdr.UUID[:], make([]byte, 16)) {
		// All zeros on new connection, impossible!
		return
	}
	if *verbose {
		log.Println("hdr", rcvHdr)
	}

	connMutex.Lock()
	mySession, ok := connMap[rcvHdr.UUID]
	connMutex.Unlock()
	if !ok {
		if *verbose {
			log.Println("unmatched uuid", rcvHdr.UUID.String())
		}
		// Session lookup failed
		if rcvHdr.Offset != -1 {
			// Unrecognized session, just close
			return
		}

		// On an initial connection, do handshake
		hostport, err := ReadLine(conn, '\n')
		if *verbose {
			log.Println("got hostport:", hostport)
		}
		if err != nil {
			log.Println("Could not find a requested endpoint")
			return
		}
		_, port, err := net.SplitHostPort(hostport)
		if err != nil {
			log.Println("Could not parse endpoint:", hostport)
			return
		}
		if p, err := strconv.Atoi(port); err != nil {
			log.Println("Could not parse port:", hostport)
			return
		} else if _, ok := allowedPorts[p]; !ok {
			log.Println("Not an allowed port:", hostport)
			return
		}

		if *verbose {
			log.Println("Dialing", hostport)
		}
		if dstConn, err := net.Dial("tcp", hostport); err == nil {
			rcvHdr.Offset = -2
			mySession = &session{
				C:    make(chan bool, 3),
				hdr:  &rcvHdr,
				conn: dstConn,
				seen: time.Now(),
			}
			// keep reads going on in the background
			go readFromDST(mySession)
			connMutex.Lock()
			connMap[rcvHdr.UUID] = mySession
			connMutex.Unlock()
		} else {
			log.Println("Could not dial requested endpoint:", hostport)
			// Cannot dial endpoint, just close
			return
		}
	} else if *verbose {
		mySession.trans.Close()
		if *verbose {
			log.Println("matched uuid", rcvHdr.UUID.String())
		}
	}
	mySession.mutex.Lock()
	defer mySession.mutex.Unlock()
	mySession.trans = conn

	if rcvHdr.Offset >= 0 && mySession.closeLocal {
		// EOF the session
		if *verbose {
			log.Println("Session is in an EOF state, sending EOF signal, closing and deleting session")
		}
		mySession.hdr.Offset = -4
		binary.Write(conn, binary.BigEndian, mySession.hdr)
		connMutex.Lock()
		delete(connMap, rcvHdr.UUID)
		connMutex.Unlock()
		return
	}

	if rcvHdr.Offset == -3 {
		// Got an EOF signal, close and delete
		// EOF the session
		if *verbose {
			log.Println("Got an EOF signal from remote, closing and deleting session")
		}
		mySession.closeLocal = true
		connMutex.Lock()
		delete(connMap, rcvHdr.UUID)
		connMutex.Unlock()
		return
	}

	err := binary.Write(conn, binary.BigEndian, mySession.hdr)
	if err != nil {
		if mySession.hdr.Offset == -2 {
			// If this is a new connection, just fail hard
			mySession.closeLocal = true
			connMutex.Lock()
			delete(connMap, rcvHdr.UUID)
			connMutex.Unlock()
		}
		return
	}

	if mySession.hdr.Offset == -2 {
		// New session has been established
		mySession.hdr.Offset = 0
	}

	if rcvHdr.Offset < mySession.bufOffset || rcvHdr.Offset > mySession.bufOffset+int64(mySession.buf.Len()) {
		log.Println("Buffer failed to maintain state", rcvHdr.Offset, "vs", mySession.bufOffset,
			"buf len:", mySession.buf.Len())
		mySession.closeLocal = true
		connMutex.Lock()
		delete(connMap, rcvHdr.UUID)
		connMutex.Unlock()
		return
	}

	// Advance the local buffer if we need to
	for rcvHdr.Offset > mySession.bufOffset {
		mySession.bufMutex.Lock()
		sz := mySession.buf.Next(2)
		tosend := mySession.buf.Next(int(sz[0])<<8 + int(sz[1]))
		mySession.bufOffset += int64(len(tosend))
		mySession.bufMutex.Unlock()
		//mySession.buf.Next(int(mySession.bufOffset - rcvHdr.Offset)) // throw away what we don't need
		//mySession.bufOffset = rcvHdr.Offset
	}

	// Do the work of the read from remote
	var localErr error
	var close bool
	go func() { // Create thread for reading with close
		defer func() {
			close = true
			mySession.C <- true
		}()

		rcvBuf := make([]byte, 1<<10)
		for !close { // infinite loop reading from DST
			n, err := conn.Read(rcvBuf)
			tosend := rcvBuf[:n]
			for len(tosend) > 0 {
				if *verbose {
					fmt.Printf("toDST %q  hoff: %d\n", tosend, mySession.hdr.Offset)
				}
				wn, writeErr := mySession.conn.Write(tosend)
				mySession.hdr.Offset += int64(wn)
				if writeErr != nil {
					mySession.closeLocal = true
					localErr = writeErr
					return
				}
				tosend = tosend[wn:]
			}
			if err != nil {
				return
			}
		}
	}()

	// Do the work of the read from local
	timer := time.NewTicker(time.Second)
	for !close && !mySession.closeLocal {
		select {
		case <-timer.C:
		case <-mySession.C:
		}
		if mySession.buf.Len() == 0 { // simulate activity / empty traffic
			_, writeErr := conn.Write([]byte{})
			if writeErr != nil {
				close = true
			}
		}
		for mySession.buf.Len() >= 2 && !close {
			mySession.bufMutex.Lock()
			b := mySession.buf.Bytes()
			sz := (int(b[0]) << 8) + int(b[1])
			tosend := b[2 : 2+sz]
			if *verbose {
				fmt.Printf("fromDST %q  off: %d buf: %d\n", tosend,
					mySession.bufOffset, mySession.buf.Len())
			}
			wn, writeErr := conn.Write(tosend)
			if close || writeErr != nil {
				// message failed to send, break connection
				if *verbose {
					log.Println("write error", writeErr)
				}
				close = true
			} else {
				mySession.buf.Next(wn + 2)
				mySession.bufOffset += int64(wn)
			}
			mySession.bufMutex.Unlock()
		}
		if mySession.closeLocal {
			if *verbose {
				log.Println("Closing conn", mySession.hdr.UUID.String())
			}
			conn.Close()
		}
		/*
			n, err := mySession.conn.Read(sendBuf)
			if err != nil {
				close = true
				mySession.conn.Close()
				mySession.conn = nil
				localErr = err
			}
			tosend := sendBuf[:n]
			for len(tosend) > 0 {
				if *verbose {
					fmt.Printf("fromDST %q  off: %d  buf: %d\n", tosend, mySession.bufOffset, mySession.buf.Len())
				}
				wn, writeErr := conn.Write(tosend)
				if close || writeErr != nil {
					// add the message left to send to the buffer and return
					mySession.buf.Write(tosend)
					close = true
					break readLocal
				}
				mySession.bufOffset += int64(wn)
				tosend = tosend[wn:]
			}*/
	}
	if localErr != nil {
		log.Println("local error", localErr)
	}
}

func readFromDST(s *session) {
	// Do the work of the read from local
	readBuf := make([]byte, 10002)
	for !s.closeLocal {
		n, err := s.conn.Read(readBuf[2:])
		if err != nil {
			if *verbose {
				log.Println("Error reading local", err)
			}
			s.closeLocal = true
		}
		if *verbose {
			fmt.Printf("fromDST %q  off: %d  buf: %d\n", readBuf[:n], s.bufOffset, s.buf.Len())
		}
		readBuf[0], readBuf[1] = byte(n>>8), byte(n&0xff)
		s.bufMutex.Lock()
		s.buf.Write(readBuf[:n+2])
		s.bufMutex.Unlock()
		if len(s.C) == 0 {
			s.C <- true
		}
	}
	if *verbose {
		log.Println("Closing local", s.hdr.UUID.String())
	}
	s.conn.Close()
}
