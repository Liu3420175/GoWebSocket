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
	"encoding/binary"
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
    maxControlFramePayloadSize = 125  // RFC Section 5.2, controlframe payloadsize <= 125, and can not be cut

	writeWait = time.Second
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
	isServer         bool     //is server to client?
	subprotocol      string  //subprotocol

	//write
	mu                chan bool
	writerBuf         []byte
	writeDeadline     time.Time
	writer            io.WriteCloser
	isWriting         bool

	writeErrMu        sync.Mutex
	writeErr          error
	enableWriteCompression bool
	compressionLevel       int
	newCompressionWriter   func(io.WriteCloser, int) io.WriteCloser


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
	// mutex lock
	err = hideTempErr(err)
	c.writeErrMu.Lock()
	if c.writeErr == nil {
		c.writeErr = err
	}
	c.writeErrMu.Unlock()
	return err
}


func (c *Conn) SetWriteDeadline(t time.Time) error {
	 c.writeDeadline = t
	 return nil
}


func (c *Conn) write(frameType int, deadline time.Time,buf0 []byte,buf1 []byte) error {
	// TODO hard to understand
	<-c.mu
	defer func() {c.mu <- true}()

	c.writeErrMu.Lock()
	err := c.writeErr
	c.writeErrMu.Unlock()
	if err != nil {
		return err
	}

	c.conn.SetWriteDeadline(deadline)
	if len(buf1) == 0{
		_,err = c.conn.Write(buf0)
	}else{
         err = c.writeBufs(buf0,buf1)
	}

	if err != nil {
		return c.WriteFatal(err)
	}

	if frameType == CloseMessage {
		c.WriteFatal(ErrCloseSent)
	}
   return nil
}

func (c *Conn) preWrite(messageType int) error {
   // check connection status before writing
   if c.writeErr != nil {
   	    c.conn.Close()
   	    c.writer = nil
   }

   if !isControl(messageType)  && !isData(messageType) {
   	    return errBadWriteOpCode
   }
   c.writeErrMu.Lock()
   err := c.writeErr
   c.writeErrMu.Unlock()
   return err

}


//write control Frame
func (c *Conn) WriteControl(messageType int, data []byte, deadline time.Time) error {
    if !isControl(messageType){
    	return errBadWriteOpCode
	}

	data_len := len(data)
    if data_len > maxControlFramePayloadSize {//控制帧数据长度不能超过125
    	return errInvalidControlFrame
	}

	b0 := byte(messageType) | finBit
	b1 := byte(data_len)

	if !c.isServer { // client to server ,Mask bit must be 1
		b1 |= maskBit
	}

	buf := make([]byte,0,maxFrameHeaderSize + maxControlFramePayloadSize) // ControlFrmae max size
	buf = append(buf,b0,b1)

	if c.isServer { // if server to client ,Mask-bit is 0
		buf = append(buf,data...)
	}else{
        newmask := newMaskKey()
        buf = append(buf,newmask[:]...)
        buf = append(buf,data...)
	}

	d := time.Hour * 1000
	if !deadline.IsZero() {
        d = deadline.Sub(time.Now()) //计算时间差
        if d < 0 {
        	return errWriteTimeout
		}
	}

	timer := time.NewTimer(d) //timer 定时器
	select { // multiplex,can recept or send
	case <-c.mu: //recept
		timer.Stop()

	case <-timer.C:
		return errWriteTimeout

	}
	defer func() {c.mu <- true} ()

	c.writeErrMu.Lock()
	err := c.writeErr
    c.writeErrMu.Unlock()

    if err != nil {
    	return err
	}

	c.conn.SetWriteDeadline(deadline)
	_,err = c.conn.Write(buf)  // TODO have some question at this
	if err != nil {  //write error info to struct
		return c.WriteFatal(err)
	}

	if messageType == CloseMessage {
		c.WriteFatal(ErrCloseSent) // 如果发送了关闭帧，就不能再发送其它的数据帧
	}
    return err
}


type messageWriter struct {
	c         *Conn
	compress  bool  // TODO do not know
	pos       int  // end of data in writeBuf
	frameType int
	err       error
}

func (message *messageWriter) fatal(err error) error {
	 if message.err != nil{
	 	message.err = err
	 	message.c.writer = nil
	 }
	 return err
}

func (message *messageWriter) flushFrame(final bool,extra []byte) error {

    c := message.c
    length := message.pos - maxFrameHeaderSize + len(extra) //

    if isControl(message.frameType) && (!final || length > maxControlFramePayloadSize){
    	return message.fatal(errInvalidControlFrame)
	}

	b0 := byte(message.frameType)
	if final {
		b0 |= finBit
	}

	if message.compress {
		b0 |= rsv1Bit
	}

	message.compress = false

	b1 := byte(0)

	if !c.isServer {
		b1 |= maskBit
	}

	framePos := 0// Assume that the frame starts at beginning of c.writeBuf.


	if c.isServer{
		// if is server ,
		framePos = 4 // TODO why? 因为在创建Conn对象初始化的时候，writerBuf的初始化是按照header的最大长度来初始化的，所以它这里的处理方法是，若实际要使用的header长度小于
		             // TODO 最大长度，前面的忽略掉，从差值处开始格式化数据，这也是一种很巧妙的处理方式
	}

	switch  {
	case length >= 65536 : // if payload-len = 127,length >= 2^16
        c.writerBuf[framePos] = b0
        c.writerBuf[framePos + 1] = b1 | 127
        binary.BigEndian.PutUint64(c.writerBuf[framePos+2:],uint64(length))//转成网络字节，因为是64位表示长度，所以用PutUnit64，将length按照64位规则写入writerBuf里
	case length > 125 : // payload-len = 126
	    framePos += 6
	    c.writerBuf[framePos] = b0
	    c.writerBuf[framePos + 1] = b1 | 126
	    binary.BigEndian.PutUint16(c.writerBuf[framePos+2:],uint16((length)))
	default:
        framePos += 8
        c.writerBuf[framePos] = b0
        c.writerBuf[framePos+1] = b1 | byte(length)
	}

	if !c.isServer {
		maskkey := newMaskKey()
		copy(c.writerBuf[maxFrameHeaderSize-4:],maskkey[:])
		//maskBytes(key, 0, c.writeBuf[maxFrameHeaderSize:w.pos])
		if len(extra) > 0{
			return c.WriteFatal(errors.New("websocket : internal error,extra used in client mode"))
		}
	}

	if c.isWriting {
		panic("concurrent write to websocket connection")
	}

	c.isWriting = true

	err := c.write(message.frameType,c.writeDeadline,c.writerBuf[framePos:message.pos],extra)

	if !c.isWriting {
		panic("concurrent write to websocket connection")
	}

	c.isWriting = false

	if err != nil {
		return message.fatal(err)
	}

	if final {
		c.writer = nil
		return nil
	}

	// setting next frame
	message.pos = maxFrameHeaderSize
	message.frameType = continuationFrame
	return nil
}


func (message *messageWriter) ncopy(max int) (int , error) {
	// TODO what to do ?copy?
	n := len(message.c.writerBuf) - message.pos //判断当前写缓存长度与消息当前偏移的差
	if n <= 0{
		if err := message.flushFrame(false,nil);err != nil { //rewrite header
			return 0,err
		}
		n = len(message.c.writerBuf) - message.pos
	}

	if n > max {
		n = max
	}
	return n,nil


}

func (message *messageWriter) Write(p []byte) (n int ,err error){
 // write p to conn
    if message.err != nil{
    	return 0,message.err
	}

	if len(p) > 2*len(message.c.writerBuf) && message.c.isServer { // TODO why?
		err := message.flushFrame(false, p)
		if err != nil {
			return 0, err
		}
		return len(p), nil
	}

	nn := len(p)
	if len(p) > 0 {
		n,err := message.ncopy(len(p))
		if err != nil {
			return 0,err
		}
		copy(message.c.writerBuf[message.pos:],p[:n])
		message.pos += n
		p = p[n:]
	}
	return nn,nil

}



func (message *messageWriter) WriteStrings(p string) (int ,error ) {
	if message.err != nil {
		return 0 , message.err
	}

	nn := len(p)
	if len(p) > 0 {
		n , err := message.ncopy(len(p))
		if err != nil {
			return 0,err
		}
		copy(message.c.writerBuf[message.pos:],p[:n])
		p = p[n:]
	}
	return nn , nil
}


func (message *messageWriter) ReadForm(r io.Reader) (nn int64, err error) {
    // read conn bufer
    if message.err != nil {
    	return 0,message.err
	}

	for {
		if message.pos == len(message.c.writerBuf) {
			err = message.flushFrame(false,nil)
			if err != nil {
				break
			}
		}
		var n int
		n, err = r.Read(message.c.writerBuf[message.pos:])
		message.pos += n
		nn += int64(n)
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			break
		}
	}
	return nn,err
}



//func (c *Conn) WritePreparedMessage(pm *Prep)



func (message *messageWriter) Close() error {
	  if message.err != nil {
	  	return message.err
	  }
	  if err:= message.flushFrame(true,nil);err != nil {
	  	return err
	  }
	  message.err = errWriteClosed
	  return nil

}

func (c *Conn) NextWriter(messageType int ) (io.WriteCloser,error){
	// if connection frame
	if err := c.preWrite(messageType); err != nil {
		return nil,err
	}
    mw := &messageWriter{
    	c:     c,
    	frameType: messageType,
    	pos: maxFrameHeaderSize,
	}
	c.writer = mw // must define function Write and Close

	return c.writer,nil
}


func (c *Conn) WriteMessage(messageType int,data []byte) error {

	if c.isServer && (c.newCompressionWriter == nil || !c.enableWriteCompression) {

		if err := c.preWrite(messageType) ; err != nil {
			return err
		}
		mw := messageWriter{c:c,frameType:messageType,pos:maxFrameHeaderSize}
		n := copy(c.writerBuf[mw.pos:],data)
		mw.pos += n
		data = data[n:]
		return mw.flushFrame(true,data) // TODO why?just one frame?
	}

	message , err := c.NextWriter(messageType)
    if err != nil {
    	return err
	}

	if _, err := message.Write(data) ; err != nil {
		return err
	}
	return message.Close()
}




//Read method


type messageReader struct {
	c *Conn
}


func (c *Conn) NextReader() (messageType int,r io.Reader , err error){
	// 读取继续帧的数据



}


func (message *messageReader) Close() error{
	return nil
}


func (c *Conn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}


func (c *Conn) setReadLimit(limit int64) {
     c.readLimit = limit
}


func (c *Conn) CloseHandler() func(code int ,text string) error { //函数的返回值可以是函数
	return c.handleClose
}


func (c *Conn) PingHandler() func (text string) error {
	return c.handlePing
}

func (c *Conn) PongHandler () func(text string ) error {
	return c.handlePong
}


func (c *Conn) handleProtocolError(message string) error {
	// 错误协议
    c.WriteControl(CloseMessage,FormatCloseMeaasge(CloseProtocolError,message),time.Now().Add(writeWait))
	return errors.New("websocket: " + message)
}

func (c *Conn) SetCloseHandler( h func(code int ,text string) error) {
	//设置关闭帧函数
	//  这个写法很经典啊,匿名函数
	 if h == nil {
	 	h = func(code int ,text string) error {
	 		message := FormatCloseMeaasge(code,"")
            err := c.WriteControl(CloseMessage,message,time.Now().Add(writeWait))
            return err
		}
	 }
	 c.handleClose = h
}


func (c *Conn) SetPingHandler(h func(text string) error) {
	if h == nil {
		h = func(message string) error {
			err := c.WriteControl(PingMessage,[]byte(message),time.Now().Add(writeWait))
			if err == ErrCloseSent {// 如果早已经接受到关闭帧，则不需要再发送Pong帧
				return nil
			}else if e, ok := err.(net.Error); ok && e.Temporary() {
				return nil
			}
			return err
		}
	}
    c.handlePing = h
}


func (c *Conn) SetPongHandler (h func(text string) error) {
	if h == nil {
		h = func(message string) error {
			//err := c.WriteControl(PongMessage,[]byte(message),time.Now().Add(writeWait))
            return nil
		}
	}
	c.handlePong = h
}

func FormatCloseMeaasge(closeCode int,text string) []byte {
	//格式化关闭状态码，关闭帧可以包含一个内容体即帧的应用数据部分，指示关闭的原因
	// 内容体头两个字节是无符号整数，网络字节序，也就是状态码
	//后面就是关闭原因解释
	if closeCode == CloseNoStatusReceived {
		return []byte{}
	}
	buf := make([]byte,len(text) +2) // apply buffer
	binary.BigEndian.PutUint16(buf,uint16(closeCode))
	copy(buf[2:], text)
	return buf

   return buf
}