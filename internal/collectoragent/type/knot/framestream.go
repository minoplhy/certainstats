package knot

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Frame Streams (https://github.com/farsightsec/fstrm), the transport dnstap
// uses. A data frame is a big-endian uint32 length and the payload. A zero
// length escapes a control frame: its own uint32 length, a uint32 type, then
// fields of uint32 type, uint32 length and bytes. Knot connects as a
// bidirectional writer: READY, our ACCEPT, START, data, STOP, our FINISH.
const (
	ctrlAccept = 0x01
	ctrlStart  = 0x02
	ctrlStop   = 0x03
	ctrlReady  = 0x04
	ctrlFinish = 0x05

	fieldContentType = 0x01

	dnstapContentType = "protobuf:dnstap.Dnstap"

	maxControlFrame = 512
	maxDataFrame    = 256 << 10
)

var errStopped = errors.New("frame stream stopped")

type control struct {
	typ          uint32
	contentTypes []string
}

// readFrame returns a data frame, or a control frame when data is nil.
func readFrame(r *bufio.Reader) (data []byte, ctrl *control, err error) {
	var n [4]byte
	if _, err := io.ReadFull(r, n[:]); err != nil {
		return nil, nil, err
	}
	size := binary.BigEndian.Uint32(n[:])
	if size > 0 {
		if size > maxDataFrame {
			return nil, nil, fmt.Errorf("dnstap frame of %d bytes is too large", size)
		}
		data = make([]byte, size)
		_, err := io.ReadFull(r, data)
		return data, nil, err
	}
	if _, err := io.ReadFull(r, n[:]); err != nil {
		return nil, nil, err
	}
	size = binary.BigEndian.Uint32(n[:])
	if size < 4 || size > maxControlFrame {
		return nil, nil, fmt.Errorf("bad dnstap control frame length %d", size)
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, nil, err
	}
	c := &control{typ: binary.BigEndian.Uint32(body)}
	for rest := body[4:]; len(rest) > 0; {
		if len(rest) < 8 {
			return nil, nil, errors.New("truncated dnstap control field")
		}
		ft, fl := binary.BigEndian.Uint32(rest), binary.BigEndian.Uint32(rest[4:])
		rest = rest[8:]
		if uint32(len(rest)) < fl {
			return nil, nil, errors.New("truncated dnstap control field")
		}
		if ft == fieldContentType {
			c.contentTypes = append(c.contentTypes, string(rest[:fl]))
		}
		rest = rest[fl:]
	}
	return nil, c, nil
}

func writeControl(w io.Writer, typ uint32, contentType string) error {
	body := binary.BigEndian.AppendUint32(nil, typ)
	if contentType != "" {
		body = binary.BigEndian.AppendUint32(body, fieldContentType)
		body = binary.BigEndian.AppendUint32(body, uint32(len(contentType)))
		body = append(body, contentType...)
	}
	frame := binary.BigEndian.AppendUint32(nil, 0)
	frame = binary.BigEndian.AppendUint32(frame, uint32(len(body)))
	frame = append(frame, body...)
	_, err := w.Write(frame)
	return err
}

// serveStream runs the reader side of one Frame Streams connection and hands
// each data frame to deliver. It returns errStopped after a clean STOP.
func serveStream(rw io.ReadWriter, deliver func([]byte)) error {
	r := bufio.NewReaderSize(rw, 64<<10)
	_, c, err := readFrame(r)
	if err != nil {
		return err
	}
	if c == nil {
		return errors.New("dnstap stream did not start with a control frame")
	}
	bidirectional := false
	switch c.typ {
	case ctrlReady:
		if !offers(c.contentTypes) {
			return fmt.Errorf("dnstap writer offered %v, not %s", c.contentTypes, dnstapContentType)
		}
		bidirectional = true
		if err := writeControl(rw, ctrlAccept, dnstapContentType); err != nil {
			return err
		}
		if _, c, err = readFrame(r); err != nil {
			return err
		}
		if c == nil || c.typ != ctrlStart {
			return errors.New("dnstap writer did not send START")
		}
	case ctrlStart:
	default:
		return fmt.Errorf("unexpected dnstap control frame %d", c.typ)
	}
	for {
		data, c, err := readFrame(r)
		if err != nil {
			return err
		}
		if c == nil {
			deliver(data)
			continue
		}
		if c.typ == ctrlStop {
			if bidirectional {
				_ = writeControl(rw, ctrlFinish, "")
			}
			return errStopped
		}
	}
}

func offers(types []string) bool {
	if len(types) == 0 {
		return true // a writer that names no type sends the default, dnstap
	}
	for _, t := range types {
		if t == dnstapContentType {
			return true
		}
	}
	return false
}
