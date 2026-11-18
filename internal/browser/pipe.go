// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package browser

import (
	"bufio"
	"io"
	"os"
	"sync"
)

// transport carries DevTools messages: a WebSocket on 127.0.0.1 (wsConn) or
// the browser's own pipe (pipeConn).
type transport interface {
	ReadMessage() ([]byte, error)
	WriteText([]byte) error
	Close() error
}

// pipeConn is --remote-debugging-pipe: hpk writes commands into one pipe and
// reads answers from another, each message ending in a NUL byte. Unlike a TCP
// port, no other program on the machine (and no other OS account) can connect
// to it: only hpk holds the other ends.
type pipeConn struct {
	r  *bufio.Reader
	rc io.Closer
	w  io.WriteCloser
	mu sync.Mutex
}

func newPipeConn(r io.ReadCloser, w io.WriteCloser) *pipeConn {
	return &pipeConn{r: bufio.NewReaderSize(r, 1<<16), rc: r, w: w}
}

func (p *pipeConn) ReadMessage() ([]byte, error) {
	b, err := p.r.ReadBytes(0)
	if err != nil {
		return nil, err
	}
	return b[:len(b)-1], nil
}

func (p *pipeConn) WriteText(b []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	buf := make([]byte, len(b)+1)
	copy(buf, b)
	_, err := p.w.Write(buf)
	return err
}

func (p *pipeConn) Close() error {
	_ = p.w.Close()
	return p.rc.Close()
}

// devtoolsPipes makes the two pipes: the browser reads commands from in and
// writes answers to out. hpk keeps the other ends.
type devtoolsPipes struct {
	childIn, childOut *os.File // the browser's ends
	ourOut, ourIn     *os.File // hpk writes to ourOut, reads from ourIn
}

func newDevtoolsPipes() (*devtoolsPipes, error) {
	cr, hw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	hr, cw, err := os.Pipe()
	if err != nil {
		cr.Close()
		hw.Close()
		return nil, err
	}
	return &devtoolsPipes{childIn: cr, childOut: cw, ourOut: hw, ourIn: hr}, nil
}

// afterStart closes the browser's ends in hpk.
func (d *devtoolsPipes) afterStart() {
	d.childIn.Close()
	d.childOut.Close()
}

func (d *devtoolsPipes) closeAll() {
	for _, f := range []*os.File{d.childIn, d.childOut, d.ourOut, d.ourIn} {
		f.Close()
	}
}
