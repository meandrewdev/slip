package slip

import (
	"bytes"
	"io"
	"sync"
)

type Reader struct {
	mu sync.Mutex
	r  io.Reader
}

func NewReader(reader io.Reader) *Reader {
	return &Reader{
		mu: sync.Mutex{},
		r:  reader,
	}
}

type Writer struct {
	mu sync.Mutex
	w  io.Writer
}

func NewWriter(writer io.Writer) *Writer {
	return &Writer{
		mu: sync.Mutex{},
		w:  writer,
	}
}

const (
	END     = 192 /* 0xC0 indicates end of packet */
	ESC     = 219 /* 0xDB, indicates byte stuffing */
	ESC_END = 220 /* 0xDC, ESC ESC_END means END data byte */
	ESC_ESC = 221 /* 0xDD, ESC ESC_ESC means ESC data byte */
)

func (s *Writer) WritePacket(p []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	buf := &bytes.Buffer{}

	/* send an initial END character to flush out any data that may
	* have accumulated in the receiver due to line noise
	 */
	if err := buf.WriteByte(END); err != nil {
		return err
	}

	/* for each byte in the packet, send the appropriate character
	 * sequence
	 */
	for _, b := range p {
		switch b {
		/* if it's the same code as an END character, we send a
		 * special two character code so as not to make the
		 * receiver think we sent an END
		 */
		case END:
			if err := buf.WriteByte(ESC); err != nil {
				return err
			}
			if err := buf.WriteByte(ESC_END); err != nil {
				return err
			}

		/* if it's the same code as an ESC character,
		 * we send a special two character code so as not
		 * to make the receiver think we sent an ESC
		 */
		case ESC:
			if err := buf.WriteByte(ESC); err != nil {
				return err
			}
			if err := buf.WriteByte(ESC_ESC); err != nil {
				return err
			}

		/* otherwise, we just send the character
		 */
		default:
			if err := buf.WriteByte(b); err != nil {
				return err
			}
		}
	}

	/* tell the receiver that we're done sending the packet
	 */
	if err := buf.WriteByte(END); err != nil {
		return err
	}

	_, err := s.w.Write(buf.Bytes())
	return err
}

/* RECV_PACKET: receives a packet into the buffer located at "p".
 *      If more than len bytes are received, the packet will
 *      be truncated.
 *      Returns the number of bytes stored in the buffer.
 */
func (s *Reader) ReadPacket() (p []byte, isPrefix bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	buf := &bytes.Buffer{}
	readBuf := make([]byte, 1)

	/* sit in a loop reading bytes until we put together
	 * a whole packet.
	 * Make sure not to copy them into the packet if we
	 * run out of room.
	 */
	for {
		/* get a character to process
		 */
		n, err = s.r.Read(readBuf)
		if n == 0 || err != nil {
			isPrefix = true
			p = buf.Bytes()
			return
		}

		/* handle bytestuffing if necessary
		 */
		switch readBuf[0] {

		/* if it's an END character then we're done with
		 * the packet
		 */
		case END:
			/* a minor optimization: if there is no
			 * data in the packet, ignore it. This is
			 * meant to avoid bothering IP with all
			 * the empty packets generated by the
			 * duplicate END characters which are in
			 * turn sent to try to detect line noise.
			 */
			if buf.Len() > 0 {
				p = buf.Bytes()
				isPrefix = false
				return
			} else {
				continue
			}

		/* if it's the same code as an ESC character, wait
		 * and get another character and then figure out
		 * what to store in the packet based on that.
		 */
		case ESC:
			n, err = s.r.Read(readBuf)

			if n == 0 || err != nil {
				isPrefix = true
				p = buf.Bytes()
				return
			}

			/* if "c" is not one of these two, then we
			 * have a protocol violation.  The best bet
			 * seems to be to leave the byte alone and
			 * just stuff it into the packet
			 */
			switch readBuf[0] {
			case ESC_END:
				readBuf[0] = END
			case ESC_ESC:
				readBuf[0] = ESC
			}
		}

		/* here we fall into the default handler and let
		 * it store the character for us
		 */
		buf.WriteByte(readBuf[0])
	}
}
