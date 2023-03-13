package main

import (
	"errors"
	"io"

	"github.com/google/uuid"
)

type ConnHeader struct {
	UUID   uuid.UUID
	Offset int64
}

// Read one character at a time and return the string slurped in with a maximum size
func ReadLine(r io.Reader, end byte) (ret string, err error) {
	var (
		n, i int
		buf  = make([]byte, 1025)
	)
	n, err = r.Read(buf[i : i+1])
	for err == nil && n == 1 && i < 1024 && buf[i] != end {
		i++
		n, err = r.Read(buf[i : i+1])
	}
	if i == 1024 {
		err = errors.New("Too long of a line")
	} else if i > 0 {
		if buf[i-1] == '\r' {
			i-- // strip the carrage return
		}
	}
	ret = string(buf[:i])
	return
}
