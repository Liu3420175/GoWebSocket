package websocket

import (
	"errors"
	"strconv"

	"io"
	"math/rand"
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
   return [4]byte{byte(n),byte(n >> 8),byte(n >> 16),byte(n >> 24)}

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
