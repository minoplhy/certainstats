package knot

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// Knot's control protocol (libknot/control/control.h), spoken over knot.sock.
// A message is a sequence of units. Each unit starts with a type byte; DATA and
// EXTRA units carry items, each a code byte (dataOffset + index), a big-endian
// uint16 length and that many bytes. A request is one DATA unit and a BLOCK; the
// reply is DATA/EXTRA units ending in a BLOCK. END closes the session.
const (
	typeEnd   byte = 0
	typeData  byte = 1
	typeExtra byte = 2
	typeBlock byte = 3

	dataOffset = 16
)

// Item indexes, in libknot's order.
const (
	idxCmd = iota
	idxFlags
	idxError
	idxSection
	idxItem
	idxID
	idxZone
	idxOwner
	idxTTL
	idxType
	idxData
	idxFilter
	idxCount
)

// allowed is every command the collector may send. Knot's socket has no
// read-only mode, so this list is the only thing between the collector and
// commands such as stop or zone-purge; it must stay read-only.
var allowed = map[string]bool{
	"status":      true,
	"stats":       true,
	"zone-stats":  true,
	"zone-status": true,
}

// maxUnits bounds one reply, so a confused peer cannot grow memory without limit.
const maxUnits = 2_000_000

// unit is one DATA or EXTRA unit of a reply.
type unit struct {
	typ  byte
	data [idxCount]string
	set  [idxCount]bool
}

func (u unit) get(i int) string { return u.data[i] }

// ctlError is an error Knot reported for the whole command.
type ctlError struct{ msg string }

func (e *ctlError) Error() string { return "Knot: " + e.msg }

// ctl talks to one Knot control socket, one connection per call.
type ctl struct {
	path    string
	timeout time.Duration
}

// request is the items of one command, by index.
type request map[int]string

// call sends one allowed command and returns the reply units.
func (c *ctl) call(ctx context.Context, req request) ([]unit, error) {
	cmd := req[idxCmd]
	if !allowed[cmd] {
		return nil, fmt.Errorf("command %q is not allowed; the collector only reads from Knot", cmd)
	}
	timeout := c.timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.path)
	if err != nil {
		return nil, fmt.Errorf("cannot open Knot's control socket %s: %w", c.path, err)
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	w := bufio.NewWriter(conn)
	if err := writeRequest(w, req); err != nil {
		return nil, err
	}
	if err := w.Flush(); err != nil {
		return nil, fmt.Errorf("writing to Knot: %w", err)
	}
	units, err := readReply(bufio.NewReader(conn))
	// END tells Knot this session is over; a failure here changes nothing.
	_, _ = conn.Write([]byte{typeEnd})
	if err != nil {
		return nil, err
	}
	// An error without a zone is about the command; per-zone errors are data.
	// Knot copies the request into an error unit (ctl_send_error), so a
	// command-wide error carries no zone. When a command runs over every zone
	// and some fail, Knot sends the good zones first and that error last; the
	// units come back with the error so zone commands can keep what arrived.
	var data []unit
	var cmdErr *ctlError
	for _, u := range units {
		if u.set[idxError] && !u.set[idxZone] {
			if cmdErr == nil {
				cmdErr = &ctlError{msg: u.get(idxError)}
			}
			continue
		}
		data = append(data, u)
	}
	if cmdErr != nil {
		return data, cmdErr
	}
	return data, nil
}

// partial reports whether err is a command error that still came with data,
// as when a command over every zone fails for some of them.
func partial(units []unit, err error) bool {
	var ce *ctlError
	return errors.As(err, &ce) && len(units) > 0
}

func writeRequest(w io.Writer, req request) error {
	buf := []byte{typeData}
	for i := 0; i < idxCount; i++ {
		v, ok := req[i]
		if !ok {
			continue
		}
		if len(v) > 0xffff {
			return fmt.Errorf("control item %d is too long", i)
		}
		buf = append(buf, byte(dataOffset+i))
		buf = binary.BigEndian.AppendUint16(buf, uint16(len(v)))
		buf = append(buf, v...)
	}
	buf = append(buf, typeBlock)
	_, err := w.Write(buf)
	return err
}

// readReply reads units until a BLOCK or END. knotd fills every field of every
// unit, EXTRA ones included (zone_status and ctl_dump_ctr reuse one data
// array), so each unit is read on its own, as knot_ctl_receive does.
func readReply(r *bufio.Reader) ([]unit, error) {
	var units []unit
	for {
		t, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("reading from Knot: %w", err)
		}
		switch t {
		case typeBlock, typeEnd:
			return units, nil
		case typeData, typeExtra:
		default:
			return nil, fmt.Errorf("unexpected control unit type %d from Knot", t)
		}
		u := unit{typ: t}
		for {
			code, err := r.Peek(1)
			if err != nil {
				return nil, fmt.Errorf("reading from Knot: %w", err)
			}
			if code[0] < dataOffset {
				break
			}
			_, _ = r.ReadByte()
			idx := int(code[0]) - dataOffset
			if idx >= idxCount {
				return nil, fmt.Errorf("unknown control item %d from Knot", idx)
			}
			var n [2]byte
			if _, err := io.ReadFull(r, n[:]); err != nil {
				return nil, fmt.Errorf("reading from Knot: %w", err)
			}
			val := make([]byte, binary.BigEndian.Uint16(n[:]))
			if _, err := io.ReadFull(r, val); err != nil {
				return nil, fmt.Errorf("reading from Knot: %w", err)
			}
			u.data[idx], u.set[idx] = string(val), true
		}
		units = append(units, u)
		if len(units) > maxUnits {
			return nil, errors.New("reply from Knot is too large")
		}
	}
}
