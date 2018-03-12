package websocket

import (
	"errors"
	"strconv"

	"io"
	"math/rand"
	"net"
	"time"
	"sync"
	"bufio"
)

//Close codes defined in RFC 6455

const (
	CloseNormalClosure           = 1000
	CloseGoingAway               = 1001
	CloseProtocolError           = 1002
	CloseUnsupportedData         = 1003
	CloseNoStatusReceived        = 1005
	CloseAbnormalClosure         = 1006
	CloseInvalidFramePayloadData = 1007
	ClosePolicyViolation         = 1008
	CloseMessageTooBig           = 1009
	CloseMandatoryExtension      = 1010
	CloseInternalServerErr       = 1011
	CloseServiceRestart          = 1012
	CloseTryAgainLater           = 1013
	CloseTLSHandshake            = 1015

)


//Message type

const (

	TextMessage   = 1
	BinaryMessage = 2
	CloseMessage  = 8
	PingMessage   = 9
	PongMessage   = 10
)


//define frame data
const (
	// Big Endia
	// 0 bits
	finBit     = 1 << 7
	rsv1Bit    = 1 << 6
	rsv2Bit    = 1 << 5
	rsv3Bit    = 1 << 4

	// 1bits
	maskBit    = 1 << 7

	maxFrameHeaderSize         = 2 + 8 + 4  //not inculde payload data,Fixde header + data max length + masking key
    maxControlFramePayloadSize = 125

    defaultReadBufferSize  = 4096
    defaultWriteBuffersize = 4096

    continuationFrame = 0 // opcode = 0. continuation frame
    noFrame           = -1
)


//define Error
 // Returned when the application writes a message to the connection after send a close message
var ErrCloseSent = errors.New("websocket: close sent")

var ErrReadLimt = errors.New("websocket:read limit exceeded")


type CloseError struct {
	// close code detail info
	Code   int
	Text   string
}


func (e * CloseError) Error() string {
    s := []byte("websocket: close")
    s = strconv.AppendInt(s,int64(e.Code),10)
	switch e.Code {
	case CloseNormalClosure:
		s = append(s,"(normal)"...)
	case CloseGoingAway:
		s = append(s,"(going away)"...)
	case CloseProtocolError:
		s = append(s,"(protocol error)"...)
	case CloseUnsupportedData:
		s = append(s,"(unsuported data)"...)
	case CloseNoStatusReceived:
		s = append(s,"(no status)"...)
	case CloseAbnormalClosure:
		s = append(s,"(abbormal closure)"...)
	case CloseInvalidFramePayloadData:
		s = append(s,"(invalid payload data)"...)
	case ClosePolicyViolation:
		s = append(s, " (policy violation)"...)
	case CloseMessageTooBig:
		s = append(s, " (message too big)"...)
	case CloseMandatoryExtension:
		s = append(s, " (mandatory extension missing)"...)
	case CloseInternalServerErr:
		s = append(s, " (internal server error)"...)
	case CloseTLSHandshake:
		s = append(s, " (TLS handshake error)"...)
	}

	if e.Text != ""{
		s = append(s,": "...)
		s = append(s,e.Text...)
	}
	return string(s)
}


func IsCloseErr(err error,codes ...int) bool {
	//error is interface object
	if e,ok := err.(*CloseError);ok { // type assertion
        for _,code := range codes {
        	if e.Code == code{
        		return true
			}
		}
	}
    return false
}


func IsUnexpectedClose(err error,expectedCodes ...int) bool {
	if e, ok := err.(*CloseError); ok {
		for _, code := range expectedCodes {
			if e.Code == code {
				return false
			}
		}
		return true
	}
	return false
}


type netError struct {
	msg         string
	temporary   bool
	timeout     bool
}

func (e *netError) Error() string {return e.msg}
func (e *netError) Temporary() bool {return e.temporary}
func (e *netError) Timeout() bool {return e.timeout}

var (
	errWriteTimeout         = &netError{msg:"websocket : write timeout",timeout:true,temporary:true}
	errUnexpectedEOF        = &CloseError{Code:CloseAbnormalClosure,Text:io.ErrUnexpectedEOF.Error()}
	errBadWriteOpCode       = errors.New("websocket: bad write message type")
	errWriteClosed          = errors.New("websocket: write close")
	errInvalidControlFrame  = errors.New("websocket: invalid control frame")
)


func newMaskKey() [4]byte {
 // new mask key
   n := rand.Uint32()
   return [4]byte{byte(n),byte(n >> 8),byte(n >> 16),byte(n >> 24)} // bit operation

}


func isControl(frameType int) bool {
	//check control frame
	return frameType == CloseMessage || frameType == PingMessage || frameType == PongMessage
}

func isData(frameType int) bool {
	//check data frame
    return frameType == TextMessage || frameType == BinaryMessage
}



var validReceivedCloseCodes = map[int]bool {
	// see http://www.iana.org/assignments/websocket/websocket.xhtml#close-code-number
	CloseNormalClosure:           true,
	CloseGoingAway:               true,
	CloseProtocolError:           true,
	CloseUnsupportedData:         true,
	CloseNoStatusReceived:        false,
	CloseAbnormalClosure:         false,
	CloseInvalidFramePayloadData: true,
	ClosePolicyViolation:         true,
	CloseMessageTooBig:           true,
	CloseMandatoryExtension:      true,
	CloseInternalServerErr:       true,
	CloseServiceRestart:          true,
	CloseTryAgainLater:           true,
	CloseTLSHandshake:            false,
}

func isValidReceivedCloseCode(code int) bool {
	return validReceivedCloseCodes[code] || (code >= 3000 && code <= 4999)
}



// wensocket connection
type Conn struct {
	conn             net.Conn // interface object
	isServer         bool
	subprotocol      string  //subprotocol

	//write
	mu                chan bool
	writerBuf         []byte
	writeDeadline     time.Time
	writer            io.WriteCloser
	isWriting         bool

	writeErrMu        sync.Mutex
	writeErr          error



	// Read
	reader            io.ReadCloser
	readErr           error
	br                *bufio.Reader
	readRemaining     int64
	readFinal         bool  //true  has continuation frame
    readLength        int64  // message size
    readLimit         int64  //Mxa meaasge size'
    readMaskPos       int    //Opcode
    readMaskKey       [4]byte

    handlePong        func(string) error
    handlePing        func(string) error
    handleClose       func(int ,string) error
    readErrCount      int


	readDecompress         bool // whether last read frame had RSV1 set
	//newDecompressionReader func(io.Reader) io.ReadCloser

}

func newConn(conn net.Conn,isServer bool,readBufferSize int,writeBufferSize int) *Conn {
	return newConnBRD(conn,isServer,readBufferSize,writeBufferSize,nil)
}

type WriteHook struct {
	p []byte
}

func (w *WriteHook) Write(p []byte) (n int, err error){
	w.p = p //write is copy
	return len(p),nil
}

func newConnBRD(conn net.Conn,isServer bool,readBufferSize int,writeBufferSize int, brw *bufio.ReadWriter ) *Conn{
	//crate Read or Write struct Conn
	//bufio.ReadWriter is struct data
	mu := make(chan bool, 1)
	mu <- true
	var br *bufio.Reader // binary reader
    if readBufferSize == 0 && brw != nil && brw.Reader != nil {
    	brw.Reader.Reset(conn)
    	if p,err := brw.Reader.Peek(0);err == nil && cap(p) >= 256 { // Peek() return slice, this time p is nil
            br = brw.Reader
		}
	}

	if br == nil {
		if readBufferSize == 0 {
			readBufferSize = defaultReadBufferSize
		}

		if readBufferSize < maxControlFramePayloadSize {
			readBufferSize = maxControlFramePayloadSize
		}

		br = bufio.NewReaderSize(conn,readBufferSize)
	}

	var writeBuf []byte
	if writeBufferSize == 0 && brw != nil && brw.Writer != nil {
		var wh WriteHook
		brw.Writer.Reset(&wh)
		brw.Writer.WriteByte(0)//writes  a single byte,why?
		brw.Flush()
		pLength := cap(wh.p)
		if  pLength >= maxFrameHeaderSize + 256 { // TODO why?
			writeBuf = wh.p[:pLength]
		}
	}

	if writeBuf == nil {
		if writeBufferSize == 0{
			writeBufferSize = defaultWriteBuffersize
		}
		writeBuf = make([]byte,writeBufferSize + maxFrameHeaderSize)
	}
    c := &Conn{
    	isServer:isServer,
    	conn:conn,
    	br:br,
    	mu:mu,
    	readFinal:true,
        writerBuf:writeBuf,
	}

	return c
}


func (c *Conn) Subprotocol() string {
	return c.subprotocol
}

func (c *Conn) Close() error {
	return c.conn.Close()
}

func (c *Conn) LocalAdrr() net.Addr {
	return c.conn.LocalAddr()
}


func (c *Conn) RemoteAddr () net.Addr {
	return c.conn.RemoteAddr()
}


func hideTempErr (err error) error {
	// hide temp error
	if e,ok := err.(net.Error);ok && e.Temporary() {
		err = &netError{msg:e.Error(),timeout:e.Timeout()}
	}
	return err
}

// write

func (c *Conn) WriteFatal (err error) error {
	err = hideTempErr(err)
	c.writeErrMu.Lock()
	if c.writeErr == nil {
		c.writeErr = err
	}
	c.writeErrMu.Unlock()
	return err
}