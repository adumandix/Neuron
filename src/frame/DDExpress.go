/**
===========================================================================
 * 基于Socket协议的服务器
===========================================================================
*/
package frame

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"model"
	"modules/serial"
	"modules/trigger"
	"modules/websocket"
	"net"
	"net/http"
	"net/url"
	"time"
)

//* ================================ DEFINE ================================ */

type ExpressS struct {
	tag   string
	brain *BrainS

	// Express[Ws]连接容器
	hub model.SyncMapHub
}

//* ================================ INNER INTERFACE ================================ */

func (express *ExpressS) WSHub() model.SyncMapHub {
	return express.hub
}

func (express *ExpressS) GMessageHandler(clientI interface{}, msgI interface{}) {
	ws := clientI.(model.SocketClient).Conn.(*websocket.Conn)
	msg, found := msgI.([]byte)
	if !found {
		express.brain.LogGenerater(model.LogError, express.tag, "GMessageHandler", "Msg not Found")
		return
	}
	if express.brain.Const.RunEnv < 2 {
		if bytes.Equal(msg, []byte("HEART")) {
			return
		}
		express.brain.LogGenerater(model.LogInfo, express.tag, fmt.Sprintf("Message[%v]", ws.Request().RemoteAddr), fmt.Sprintf("%X", msg))
	}
}

//* Websocket Handler */
func (express *ExpressS) WSHandler(ws *websocket.Conn, wsI model.WebsocketI) {
	// Define
	hub := wsI.WSHub()
	// Initialize
	bufLen := express.brain.Const.WSParam.BufferSize
	msgSlice := make([]byte, bufLen)
	var msgBuf bytes.Buffer
	defer func() {
		express.brain.LogGenerater(model.LogTrace, express.tag, hub.Tag, fmt.Sprintf("Exit Customer -> [%v] Count -> [%v]", ws.Request().RemoteAddr, hub.Len()))
		ws.Close()
		hub.Del(ws.Request().RemoteAddr)
	}()
	// Init Customer
	//* 此处Tag为空即为广义连接者 */
	hub.Set(ws.Request().RemoteAddr, model.SocketClient{Tag: "", Conn: ws})
	express.brain.LogGenerater(model.LogTrace, express.tag, hub.Tag, fmt.Sprintf("Init Customer -> [%v] Count -> [%v]", ws.Request().RemoteAddr, hub.Len()))
	// ReadHandler
	endC := make(chan map[int]interface{})
	msgC := make(chan interface{})
	defer close(endC)
	defer close(msgC)
	for {
		go express.brain.SafeFunction(func() {
			// Heart Config
			if err := ws.SetDeadline(time.Now().Add(time.Duration(express.brain.Const.WSParam.Interval+3000) * time.Millisecond)); err != nil {
				endC <- map[int]interface{}{214: fmt.Sprintf("WSHandler[Deadline] -> %v", err)}
				return
			}
			// ReadBuffer
			for {
				n, err := ws.Read(msgSlice)
				if err != nil {
					endC <- map[int]interface{}{216: fmt.Sprintf("WSHandler[Read] -> %v", err)}
					return
				}
				msgBuf.Write(msgSlice[:n])
				if n < bufLen {
					break
				}
			}
			msgC <- msgBuf.Bytes()
			msgBuf.Reset()
		}, func(err interface{}) {
			if err == nil {
				return
			}
			endC <- map[int]interface{}{204: fmt.Sprintf("WSHandler[SafeFunction] -> %v", err)}
		})
		select {
		case data := <-endC:
			for k, v := range data {
				express.brain.MessageHandler(express.tag, fmt.Sprintf("Error[%v]", hub.Tag), k, v)
			}
			return
		case data := <-msgC:
			wsI.GMessageHandler(hub.Get(ws.Request().RemoteAddr), data)
		}
	}
}

//* ================================ PRIVATE ================================ */

func (express *ExpressS) main() {
	express.hub.Init("ExpressTunnel")
}

//* TCP服务端处理程序 */
func (express *ExpressS) tcpServerHandler(conn net.Conn, mTrigger trigger.Trigger, hub model.SyncMapHub, heartInterval time.Time) {
	// Initialize
	defer func() {
		express.brain.LogGenerater(model.LogTrace, express.tag, hub.Tag, fmt.Sprintf("Exit Customer -> [%v] Count -> [%v]", conn.RemoteAddr().String(), hub.Len()))
		conn.Close()
		hub.Del(conn.RemoteAddr().String())
	}()
	// Init Customer
	//* 此处Tag为空即为广义连接者 */
	hub.Set(conn.RemoteAddr().String(), model.SocketClient{Tag: "", Conn: conn})
	express.brain.LogGenerater(model.LogTrace, express.tag, hub.Tag, fmt.Sprintf("Init Customer -> [%v] Count -> [%v]", conn.RemoteAddr().String(), hub.Len()))
	// Read Handler
	bufLen := express.brain.Const.TCPParam.BufferSize
	msgSlice := make([]byte, bufLen)
	var msgBuf bytes.Buffer
	endC := make(chan map[int]interface{})
	msgC := make(chan interface{})
	defer close(endC)
	defer close(msgC)
	for {
		go express.brain.SafeFunction(func() {
			// Heart Config
			if err := conn.SetDeadline(time.Now().Add(time.Duration(express.brain.Const.TCPParam.Interval+3000) * time.Millisecond)); err != nil {
				endC <- map[int]interface{}{210: fmt.Sprintf("tcpServerHandler[SetDeadline] -> %v", err)}
				return
			}
			// Read Buffer
			for {
				n, err := conn.Read(msgSlice)
				if err != nil {
					endC <- map[int]interface{}{216: fmt.Sprintf("tcpServerHandler[Read] -> %v", err)}
					return
				}
				msgBuf.Write(msgSlice[:n])
				if n < bufLen {
					break
				}
			}
			msgC <- msgBuf.Bytes()
			msgBuf.Reset()
		}, func(err interface{}) {
			if err == nil {
				return
			}
			endC <- map[int]interface{}{204: fmt.Sprintf("tcpServerHandler[SafeFunction] -> %v", err)}
		})
		select {
		case data := <-endC:
			for k, v := range data {
				mTrigger.FireBackground("Error", k, v)
			}
			return
		case data := <-msgC:
			mTrigger.FireBackground("Message", 100, data)
		}
	}
}

//* tcpServer转发数据控制 */
func (express *ExpressS) tcpServerForwardMessageHandler(localConn, remoteConn io.ReadWriteCloser) {
	defer localConn.Close()
	defer remoteConn.Close()
	io.Copy(remoteConn, localConn)
}

//* tcpRemote转发数据控制 */
func (express *ExpressS) tcpRemoteForwardMessageHandler(localConn io.ReadWriteCloser, data interface{}) {
	localConn.Write(data.([]byte))
}

//* UDP服务端处理程序 */
func (express *ExpressS) udpServerHandler(conn *net.UDPConn, mTrigger trigger.Trigger, hub model.ConnQHub, heartInterval time.Time) {
	// Read Handler
	bufLen := express.brain.Const.UDPParam.BufferSize
	msgSlice := make([]byte, bufLen)
	var msgBuf bytes.Buffer
	endC := make(chan map[int]interface{})
	msgC := make(chan interface{})
	defer close(endC)
	defer close(msgC)
	for {
		go express.brain.SafeFunction(func() {
			// Heart Config
			if err := conn.SetDeadline(heartInterval); err != nil {
				endC <- map[int]interface{}{211: fmt.Sprintf("udpServerHandler[SetDeadline] -> %v", err)}
				return
			}
			var udpAddr *net.UDPAddr
			// ReadBuffer
			for {
				n, addr, err := conn.ReadFromUDP(msgSlice)
				if err != nil {
					endC <- map[int]interface{}{216: fmt.Sprintf("udpServerHandler[Read] -> %v", err)}
					return
				}
				udpAddr = addr
				msgBuf.Write(msgSlice[:n])
				if n < bufLen {
					break
				}
			}
			msgC <- model.UDPPacket{
				Addr: udpAddr,
				Msg:  msgBuf.Bytes(),
			}
			msgBuf.Reset()
		}, func(err interface{}) {
			if err == nil {
				return
			}
			endC <- map[int]interface{}{204: fmt.Sprintf("udpServerHandler[SafeFunction] -> %v", err)}
		})
		select {
		case data := <-endC:
			for k, v := range data {
				mTrigger.FireBackground("Error", k, v)
			}
			return
		case data := <-msgC:
			mTrigger.FireBackground("Message", 100, data)
		}
	}
}

//* udpServer转发数据控制 */
func (express *ExpressS) udpServerForwardMessageHandler(hub model.ConnQHub, remoteConn io.ReadWriteCloser, data interface{}) {
	addr := data.(model.UDPPacket).Addr
	msgB := data.(model.UDPPacket).Msg
	if string(msgB) == "__FLUSH" {
		hub.ConnQ.Renew()
		express.brain.LogGenerater(model.LogTrace, express.tag, hub.Tag, fmt.Sprintf("Exit Customer -> [ALL] Count -> [%v] Max -> [%v]", hub.ConnQ.Len(), express.brain.Const.UDPParam.MaxLen))
	} else {
		// Init Customer
		if hub.ConnQ.Contains(addr) == nil {
			hub.ConnQ.Push(addr)
			express.brain.LogGenerater(model.LogTrace, express.tag, hub.Tag, fmt.Sprintf("Init Customer -> [%v] Count -> [%v] Max -> [%v]", addr, hub.ConnQ.Len(), express.brain.Const.UDPParam.MaxLen))
		}
		remoteConn.Write(msgB)
	}
}

//* udpRemote转发数据控制 */
func (express *ExpressS) udpRemoteForwardMessageHandler(hub model.ConnQHub, localConn *net.UDPConn, data []byte) {
	// 通过local回传本地端口
	if !hub.ConnQ.IsEmpty() {
		for e := hub.ConnQ.Front(); e != nil; e.Next() {
			localConn.WriteToUDP(data, e.Value.(*net.UDPAddr))
		}
	}
}

//* TCP端口转发[remote] */
func (express *ExpressS) tcpRemoteConn(remote string, localConn net.Conn) {
	mTrigger := trigger.New()
	mTrigger.On("Open", func(code int, data interface{}) {
		remoteConn := data.(net.Conn)
		express.brain.LogGenerater(model.LogTrace, express.tag, "tcpRemoteConn", fmt.Sprintf("Remote Open -> %v", remoteConn.RemoteAddr()))
		express.tcpServerForwardMessageHandler(localConn, remoteConn)
	})
	mTrigger.On("Message", func(code int, data interface{}) {
		express.tcpRemoteForwardMessageHandler(localConn, data)
	})
	mTrigger.On("Close", func(code int, data interface{}) {
		express.brain.LogGenerater(model.LogTrace, express.tag, "tcpRemoteConn", fmt.Sprintf("Remote Close -> %v", data))
	})
	mTrigger.On("Error", func(code int, data interface{}) {
		express.brain.LogGenerater(model.LogTrace, express.tag, "tcpRemoteConn", fmt.Sprintf("Remote Error -> %v", express.brain.MessageHandler(express.tag, "tcpRemoteConn", code, data)))
	})
	go express.TCPClient(remote, mTrigger)
}

//* UDP端口转发[remote] */
func (express *ExpressS) udpRemoteConn(remote string, localConn *net.UDPConn, hub model.ConnQHub, callback func(remoteConn *net.UDPConn)) {
	mTrigger := trigger.New()
	mTrigger.On("Open", func(code int, data interface{}) {
		remoteConn := data.(*net.UDPConn)
		express.brain.LogGenerater(model.LogTrace, express.tag, "udpRemoteConn", fmt.Sprintf("Remote Open -> %v", remoteConn.RemoteAddr()))
		callback(remoteConn)
	})
	mTrigger.On("Message", func(code int, data interface{}) {
		express.udpRemoteForwardMessageHandler(hub, localConn, data.(model.UDPPacket).Msg)
	})
	mTrigger.On("Close", func(code int, data interface{}) {
		express.brain.LogGenerater(model.LogTrace, express.tag, "udpRemoteConn", fmt.Sprintf("Remote Close -> %v", data))
	})
	mTrigger.On("Error", func(code int, data interface{}) {
		express.brain.LogGenerater(model.LogTrace, express.tag, "udpRemoteConn", fmt.Sprintf("Remote Error -> %v", express.brain.MessageHandler(express.tag, "udpRemoteConn", code, data)))
	})
	go express.UDPClient(remote, mTrigger)
}

//* TCP转UDP[remote] */
func (express *ExpressS) tcp2UDPRemoteConn(remote string, localConn *net.UDPConn, hub model.ConnQHub, callback func(remoteConn net.Conn)) {
	mTrigger := trigger.New()
	mTrigger.On("Open", func(code int, data interface{}) {
		remoteConn := data.(net.Conn)
		express.brain.LogGenerater(model.LogTrace, express.tag, "tcpRemoteConn", fmt.Sprintf("Remote Open -> %v", remoteConn.RemoteAddr()))
		callback(remoteConn)
	})
	mTrigger.On("Message", func(code int, data interface{}) {
		express.udpRemoteForwardMessageHandler(hub, localConn, data.([]byte))
	})
	mTrigger.On("Close", func(code int, data interface{}) {
		express.brain.LogGenerater(model.LogTrace, express.tag, "tcpRemoteConn", fmt.Sprintf("Remote Close -> %v", data))
	})
	mTrigger.On("Error", func(code int, data interface{}) {
		express.brain.LogGenerater(model.LogTrace, express.tag, "tcpRemoteConn", fmt.Sprintf("Remote Error -> %v", express.brain.MessageHandler(express.tag, "tcpRemoteConn", code, data)))
	})
	go express.TCPClient(remote, mTrigger)
}

//* UDP转TCP[remote] */
func (express *ExpressS) udp2TCPRemoteConn(remote string, localConn net.Conn) {
	mTrigger := trigger.New()
	mTrigger.On("Open", func(code int, data interface{}) {
		remoteConn := data.(*net.UDPConn)
		express.brain.LogGenerater(model.LogTrace, express.tag, "udpRemoteConn", fmt.Sprintf("Remote Open -> %v", remoteConn.RemoteAddr()))
		express.tcpServerForwardMessageHandler(localConn, remoteConn)
	})
	mTrigger.On("Message", func(code int, data interface{}) {
		localConn.Write(data.(model.UDPPacket).Msg)
	})
	mTrigger.On("Close", func(code int, data interface{}) {
		express.brain.LogGenerater(model.LogTrace, express.tag, "udpRemoteConn", fmt.Sprintf("Remote Close -> %v", data))
	})
	mTrigger.On("Error", func(code int, data interface{}) {
		express.brain.LogGenerater(model.LogTrace, express.tag, "udpRemoteConn", fmt.Sprintf("Remote Error -> %v", express.brain.MessageHandler(express.tag, "udpRemoteConn", code, data)))
	})
	go express.UDPClient(remote, mTrigger)
}

//* UART转UDP[remote] */
func (express *ExpressS) uart2UDPRemoteConn(remote serial.OpenOptions, localConn *net.UDPConn, hub model.ConnQHub, callback func(remoteConn io.ReadWriteCloser)) {
	mTrigger := trigger.New()
	mTrigger.On("Open", func(code int, data interface{}) {
		remoteConn := data.(io.ReadWriteCloser)
		express.brain.LogGenerater(model.LogTrace, express.tag, "uart2UDPRemoteConn", fmt.Sprintf("Remote Open -> %v", remote.PortName))
		callback(remoteConn)
	})
	mTrigger.On("Message", func(code int, data interface{}) {
		express.udpRemoteForwardMessageHandler(hub, localConn, data.(model.UDPPacket).Msg)
	})
	mTrigger.On("Close", func(code int, data interface{}) {
		express.brain.LogGenerater(model.LogTrace, express.tag, "uart2UDPRemoteConn", fmt.Sprintf("Remote Close -> %v", data))
	})
	mTrigger.On("Error", func(code int, data interface{}) {
		express.brain.LogGenerater(model.LogTrace, express.tag, "uart2UDPRemoteConn", fmt.Sprintf("Remote Error -> %v", express.brain.MessageHandler(express.tag, "uart2UDPRemoteConn", code, data)))
	})
	go express.UARTClient(remote, mTrigger)
}

//* ================================ PUBLIC ================================ */

//* 构造本体 */
func (express *ExpressS) Ontology(neuron *NeuronS) *ExpressS {
	express.tag = "Express"
	express.brain = neuron.Brain
	express.brain.SafeFunction(express.main)
	return express
}

//* StaticHandler配置 */
func (express *ExpressS) StaticHandler(res http.ResponseWriter, req *http.Request) {
	express.ConstructInterface(res, req, true, func() {
		switch req.Header.Get("Connection") {
		case "Upgrade":
			websocket.Handler(express.WSHandler).ServeHTTP(res, req, express)
			break
		default:
			http.FileServer(http.Dir(express.brain.PathAbs(express.brain.Const.HTTPServer.StaticPath))).ServeHTTP(res, req)
			break
		}
	})
}

//* Request Middleware */
/*
	Method:GET
	URL:/Commander/Channel
	Proto:HTTP/1.1
	ProtoMajor:1
	ProtoMinor:1
	Header:map[Connection:[Upgrade] Origin:[http://127.0.0.1:8800] Sec-Websocket-Key:[MI05HZpoEfjdNsND2/Evug==]
	Sec-Websocket-Version:[13]
	Upgrade:[websocket]]
	Body:{}
	GetBody:<nil>
	ContentLength:0
	TransferEncoding:[]
	Close:false
	Host:127.0.0.1:8800
	Form:map[]
	PostForm:map[]
	MultipartForm:<nil>
	Trailer:map[]
	RemoteAddr:127.0.0.1:60800
	RequestURI:/Commander/Channel
	TLS:<nil>
	Cancel:<nil>
	Response:<nil>
	ctx:0xc00002f4c0
*/
func (express *ExpressS) Middleware(res http.ResponseWriter, req *http.Request, next func()) {
	// Redirect
	//u := express.Url2Struct(express.Req2Url(req))
	//if express.brain.Const.HTTPS.Open && u.Port() == "80" {
	//	http.Redirect(res, req, express.Req2Url(req), http.StatusMovedPermanently)
	//}
	// Log
	express.brain.LogGenerater(model.LogTrace, express.tag, "Middleware", fmt.Sprintf("[Visitor] => %s [Resource] => %s %s", req.RemoteAddr, req.Method, req.URL))
	// Header
	res.Header().Set("X-Powered-By", express.brain.Const.HTTPServer.XPoweredBy)
	if express.brain.Const.HTTPServer.ACAO {
		res.Header().Set("Access-Control-Allow-Origin", "*")
	}
	next()
}

//* 获取Requst中的地址 */
func (express *ExpressS) Req2Url(req *http.Request) string {
	var scheme string
	upgrade := req.Header.Get("Upgrade")
	if upgrade == "websocket" {
		scheme = "ws://"
		if req.TLS != nil {
			scheme = "wss://"
		}
	} else {
		scheme = "http://"
		if req.TLS != nil {
			scheme = "https://"
		}
	}
	return scheme + req.Host + req.RequestURI
}

//* 获取Requst中的地址不带参数 */
func (express *ExpressS) Req2UrlNoQuery(req *http.Request) string {
	var scheme string
	upgrade := req.Header.Get("Upgrade")
	if upgrade == "websocket" {
		scheme = "ws://"
		if req.TLS != nil {
			scheme = "wss://"
		}
	} else {
		scheme = "http://"
		if req.TLS != nil {
			scheme = "https://"
		}
	}
	u, _ := url.Parse(scheme + req.Host + req.RequestURI)
	return u.Scheme + "://" + u.Host + u.Path
}

//* 通用Request数据包解析 */
func (express *ExpressS) Req2Query(req *http.Request) url.Values {
	if req.Method == "GET" {
		query, err := url.Parse(req.RequestURI)
		if err != nil {
			express.brain.MessageHandler(express.tag, "Req2Query", 207, err)
			return nil
		}
		return query.Query()
	} else if req.Method == "POST" {
		err := req.ParseForm()
		if err != nil {
			express.brain.MessageHandler(express.tag, "Req2Query", 207, err)
			return nil
		}
		return req.Form
	}
	return nil
}

//* URL对象转化 */
func (express *ExpressS) Url2Struct(ustr string) *url.URL {
	u, err := url.Parse(ustr)
	if err != nil {
		express.brain.MessageHandler(express.tag, "Url2Struct", 212, err)
		return nil
	}
	return u
}

//* 分离Url中的Host和Params */
func (express *ExpressS) Url2HostPath(uStr string) (string, string) {
	u, err := url.Parse(uStr)
	if err != nil {
		express.brain.MessageHandler(express.brain.tag, "RequestUrl2HostPath", 204, err)
	}
	scheme := u.Scheme
	if scheme != "" {
		scheme = scheme + "://"
	}
	query := u.RawQuery
	if query != "" {
		query = "?" + query
	}
	return scheme + u.Host, u.Path + query
}

//* Response -> 通用JSON格式 */
func (express *ExpressS) CodeResponse(res http.ResponseWriter, code int, data ...interface{}) {
	var content interface{}
	var function string
	// data[0] -> content
	if express.brain.CheckIsNull(data) {
		content = nil
	} else {
		content = data[0]
	}
	// data[1] -> function
	if len(data) <= 1 {
		function = "CodeResponse"
	} else {
		function = data[1].(string)
	}
	msg := express.brain.MessageHandler(express.tag, function, code, content)
	res.Write(express.brain.JsonEncoder(msg))
}

//* Response -> 通用错误格式 */
func (express *ExpressS) ErrorResponse(res http.ResponseWriter, code int) {
	switch code {
	case 500:
		res.WriteHeader(code)
		res.Write([]byte("<body style='margin:0;overflow-y:auto;'><img style='width:100%;' src='/error/500.gif' onerror='javascript:document.body.innerHTML = \"<h1>500 Server Error</h1>\"'></body>"))
		break
	default:
		res.WriteHeader(404)
		res.Write([]byte("<body style='margin:0;overflow-y:auto;'><img style='width:100%;' src='/error/404.gif' onerror='javascript:document.body.innerHTML = \"<h1>500 Server Error</h1>\"'></body>"))
		break
	}
}

//* REQ&RES -> 构建通用接口 */
func (express *ExpressS) ConstructInterface(res http.ResponseWriter, req *http.Request, isStarted bool, next func(), callbacks ...func(err interface{})) {
	if isStarted {
		express.brain.SafeFunction(func() {
			express.Middleware(res, req, func() {
				next()
			})
		}, func(err interface{}) {
			if err == nil {
				return
			}
			for _, v := range callbacks {
				v(err)
			}
		})
	} else {
		express.CodeResponse(res, 201)
	}
}

//* Service -> 构建通用服务 */
func (express *ExpressS) ConstructService(service model.ExpressI, servicePath string, res http.ResponseWriter, req *http.Request) {
	express.brain.SafeFunction(func() {
		express.Middleware(res, req, func() {
			query := express.Req2Query(req)
			neuronId := express.brain.Const.NeuronId
			if !express.brain.CheckIsNull(query[neuronId+"-start"]) {
				if !service.IsStarted() {
					// 开启服务
					service.StartService()
					express.CodeResponse(res, 101, "[Visitor] => "+req.RemoteAddr)
				} else {
					http.Redirect(res, req, servicePath, http.StatusFound)
				}
			} else if !express.brain.CheckIsNull(query[neuronId+"-stop"]) {
				if service.IsStarted() {
					// 关闭服务
					service.StopService()
					express.CodeResponse(res, 102, "[Visitor] => "+req.RemoteAddr)
				} else {
					http.Redirect(res, req, servicePath, http.StatusFound)
				}
			} else {
				// 服务状态
				if service.IsStarted() {
					express.CodeResponse(res, 101, "[Visitor] => "+req.RemoteAddr)
				} else {
					express.CodeResponse(res, 102, "[Visitor] => "+req.RemoteAddr)
				}
			}
		})
	}, func(err interface{}) {
		if err == nil {
			return
		}
		express.brain.MessageHandler(express.tag, "ConstructService -> SafeFunction", 204, err)
	})
}

//* ================================ SOCKET ================================ */

//* Websocket广播 */
func (express *ExpressS) WSBroadcast(callback func(rank int, ip string, neuronId string, conn *websocket.Conn), wsHubs ...model.SyncMapHub) {
	wsHub := express.hub
	if len(wsHubs) > 0 {
		wsHub = wsHubs[0]
	}
	if !wsHub.IsEmpty() {
		wsHub.Iterator(func(rank int, ip string, socketConn interface{}) bool {
			client, found := socketConn.(model.SocketClient)
			if !found {
				return true
			}
			conn, found := client.Conn.(*websocket.Conn)
			if !found {
				return true
			}
			callback(rank, ip, client.Tag, conn)
			return true
		})
	}
}

//* 通过CommanderQueue发送命令 */
/*
behaviorTreeQ -> map[UUID]*BehaviorTreeS
*/
func (express *ExpressS) CommanderEval(neuronId, service, function string, params ...[]byte) string {
	msgId := express.brain.UUID()
	gmsg := model.GMessageS{
		ID:   msgId,
		Head: "?",
		Tag:  "EVAL",
		Cmds: []interface{}{
			service,
			function,
		},
	}
	// 并上参数
	for _, v := range params {
		gmsg.Cmds = append(gmsg.Cmds, express.brain.Base64Encoder(v))
	}
	// 发送指令
	express.brain.Container.CommanderQueue.Push(model.CommanderPiece{NeuronId: neuronId, GMessage: gmsg})
	return msgId
}

//* Receiver返回命令 */
func (express *ExpressS) ReceiverEval(conn *websocket.Conn, msgId, service, function string, params ...[]byte) error {
	gmsg := model.GMessageS{
		ID:   msgId,
		Head: "?",
		Tag:  "EVAL",
		Cmds: []interface{}{
			service,
			function,
		},
	}
	// 并上参数
	for _, v := range params {
		gmsg.Cmds = append(gmsg.Cmds, express.brain.Base64Encoder(v))
	}
	// 发送指令
	_, err := conn.Write(express.brain.SystemEncrypt(express.brain.GenerateMessage(gmsg.Head, gmsg.Tag, gmsg.Cmds, gmsg.ID).Bytes()))
	return err
}

//* Websocket客户端 */
func (express *ExpressS) WSClient(u string, mTrigger trigger.Trigger, heartIntervals ...int) {
	if express.brain.CheckIsNull(mTrigger) {
		express.brain.MessageHandler(express.tag, "WSClient", 220, "mTrigger -> Null")
		return
	}
	defer mTrigger.FireBackground("Close", 103, fmt.Sprintf("SocketClient[Closed] -> %v", u))
	// Initialize
	heartInterval := time.Time{}
	if len(heartIntervals) > 0 {
		heartInterval = time.Now().Add(time.Duration(heartIntervals[0]) * time.Millisecond)
	}
	// Init Connection
	uParsed, _ := url.Parse(u)
	config, err := websocket.NewConfig(u, "http://"+uParsed.Host)
	if err != nil {
		mTrigger.FireBackground("Error", 212, err)
		return
	}
	config.Dialer = &net.Dialer{
		Deadline: heartInterval,
	}
	config.TlsConfig = &tls.Config{
		InsecureSkipVerify: true,
	}
	conn, err := websocket.DialConfig(config)
	if err != nil {
		mTrigger.FireBackground("Error", 210, err)
		return
	}
	defer conn.Close()
	// Open
	mTrigger.FireBackground("Open", 100, conn)
	// Read Handler
	bufLen := express.brain.Const.WSParam.BufferSize
	msgSlice := make([]byte, bufLen)
	var msgBuf bytes.Buffer
	endC := make(chan map[int]interface{})
	msgC := make(chan interface{})
	defer close(endC)
	defer close(msgC)
	for {
		go express.brain.SafeFunction(func() {
			// Read Buffer
			for {
				n, err := conn.Read(msgSlice)
				if err != nil {
					endC <- map[int]interface{}{216: fmt.Sprintf("WSClient[Read] -> %v", err)}
					return
				}
				msgBuf.Write(msgSlice[:n])
				if n < bufLen {
					break
				}
			}
			msgC <- msgBuf.Bytes()
			msgBuf.Reset()
		}, func(err interface{}) {
			if err == nil {
				return
			}
			endC <- map[int]interface{}{204: fmt.Sprintf("WSClient[SafeFunction] -> %v", err)}
		})
		select {
		case data := <-endC:
			for k, v := range data {
				mTrigger.FireBackground("Error", k, v)
			}
			return
		case data := <-msgC:
			mTrigger.FireBackground("Message", 100, data)
		}
	}
}

//* TCP服务端主程序 */
func (express *ExpressS) TCPServer(u string, mTrigger trigger.Trigger, stopC chan bool, hub model.SyncMapHub, heartIntervals ...int) {
	if express.brain.CheckIsNull(mTrigger) {
		express.brain.MessageHandler(express.tag, "TCPServer", 220, "mTrigger -> Null")
		return
	}
	defer close(stopC)
	defer mTrigger.FireBackground("Close", 103, fmt.Sprintf("TCPServer[Closed] -> %v", u))
	// Initialize
	heartInterval := time.Time{}
	if len(heartIntervals) > 0 {
		heartInterval = time.Now().Add(time.Duration(heartIntervals[0]) * time.Millisecond)
	}
	// Listen Port
	servListen, err := net.Listen("tcp", u)
	if err != nil {
		mTrigger.FireBackground("Error", 210, fmt.Sprintf("TCPServer[Listen] -> %v", err))
		return
	}
	defer servListen.Close()
	mTrigger.FireBackground("Open", 100, hub.Tag)
	go express.brain.SafeFunction(func() {
		for {
			conn, err := servListen.Accept()
			if err != nil {
				express.brain.LogGenerater(model.LogError, express.tag, "TCPServer[Accept]", fmt.Sprintf("%v", err))
				return
			}
			mTrigger.FireBackground("Accept", 100, conn)
			if mTrigger.HasEvent("Message") {
				go express.tcpServerHandler(conn, mTrigger, hub, heartInterval)
			}
		}
	})
	for {
		select {
		case data := <-stopC:
			if data {
				return
			}
		}
	}
}

//* TCP客户端 */
func (express *ExpressS) TCPClient(u string, mTrigger trigger.Trigger, heartIntervals ...int) {
	if express.brain.CheckIsNull(mTrigger) {
		express.brain.MessageHandler(express.tag, "TCPClient", 220, "mTrigger -> Null")
		return
	}
	defer mTrigger.FireBackground("Close", 103, fmt.Sprintf("SocketClient[Closed] -> %v", u))
	// Initialize
	heartInterval := time.Time{}
	if len(heartIntervals) > 0 {
		heartInterval = time.Now().Add(time.Duration(heartIntervals[0]) * time.Millisecond)
	}
	bufLen := express.brain.Const.WSParam.BufferSize
	var msgBuf bytes.Buffer
	msgSlice := make([]byte, bufLen)
	// Init Connection
	conn, err := net.Dial("tcp", u)
	if err != nil {
		mTrigger.FireBackground("Error", 210, err)
		return
	}
	defer conn.Close()
	mTrigger.FireBackground("Open", 100, conn /*[net.Conn]*/)
	// Read Handler
	endC := make(chan map[int]interface{})
	msgC := make(chan interface{})
	defer close(endC)
	defer close(msgC)
	for {
		go express.brain.SafeFunction(func() {
			// Heart Config
			if err := conn.SetDeadline(heartInterval); err != nil {
				endC <- map[int]interface{}{210: fmt.Sprintf("TCPClient[SetDeadline] -> %v", err)}
				return
			}
			// Read Buffer
			for {
				n, err := conn.Read(msgSlice)
				if err != nil {
					endC <- map[int]interface{}{216: fmt.Sprintf("TCPClient[Read] -> %v", err)}
					return
				}
				msgBuf.Write(msgSlice[:n])
				if n < bufLen {
					break
				}
			}
			msgC <- msgBuf.Bytes()
			msgBuf.Reset()
		}, func(err interface{}) {
			if err == nil {
				return
			}
			endC <- map[int]interface{}{204: fmt.Sprintf("TCPClient[SafeFunction] -> %v", err)}
		})
		select {
		case data := <-endC:
			for k, v := range data {
				mTrigger.FireBackground("Error", k, v)
			}
			return
		case data := <-msgC:
			mTrigger.FireBackground("Message", 100, data)
		}
	}
}

//* UDP服务端主程序 */
func (express *ExpressS) UDPServer(u string, mTrigger trigger.Trigger, stopC chan bool, hub model.ConnQHub, heartIntervals ...int) {
	if express.brain.CheckIsNull(mTrigger) {
		express.brain.MessageHandler(express.tag, "UDPServer", 220, "mTrigger -> Null")
		return
	}
	defer close(stopC)
	defer mTrigger.FireBackground("Close", 103, fmt.Sprintf("UDPServer[Closed] -> %v", u))
	// Initialize
	heartInterval := time.Time{}
	if len(heartIntervals) > 0 {
		heartInterval = time.Now().Add(time.Duration(heartIntervals[0]) * time.Millisecond)
	}
	// Listen Port
	addr, err := net.ResolveUDPAddr("udp", u)
	if err != nil {
		mTrigger.FireBackground("Error", 211, fmt.Sprintf("UDPServer[ResolveUDPAddr] -> %v", err))
		return
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		mTrigger.FireBackground("Error", 211, fmt.Sprintf("UDPServer[ListenUDP] -> %v", err))
		return
	}
	defer conn.Close()
	// Open Connection
	mTrigger.FireBackground("Open", 100, conn /*[*net.UDPConn]*/)
	go express.brain.SafeFunction(func() {
		express.udpServerHandler(conn, mTrigger, hub, heartInterval)
	}, func(err interface{}) {
		if err == nil {
			return
		}
		mTrigger.FireBackground("Error", 211, fmt.Sprintf("UDPServer[udpServerHandler] -> %v", err))
	})
	for {
		select {
		case data := <-stopC:
			if data {
				return
			}
		}
	}
}

//* UDP客户端[*net.UDPConn] */
func (express *ExpressS) UDPClient(u string, mTrigger trigger.Trigger, heartIntervals ...int) {
	if express.brain.CheckIsNull(mTrigger) {
		express.brain.MessageHandler(express.tag, "UDPClient", 220, "mTrigger -> Null")
		return
	}
	defer mTrigger.FireBackground("Close", 103, fmt.Sprintf("UDPClient[Closed] -> %v", u))
	// Initialize
	heartInterval := time.Time{}
	if len(heartIntervals) > 0 {
		heartInterval = time.Now().Add(time.Duration(heartIntervals[0]) * time.Millisecond)
	}
	bufLen := express.brain.Const.UDPParam.BufferSize
	msgSlice := make([]byte, bufLen)
	// Init Connection
	addr, err := net.ResolveUDPAddr("udp", u)
	if err != nil {
		mTrigger.FireBackground("Error", 211, fmt.Sprintf("ResolveUDPAddr -> [%v]", err))
		return
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		mTrigger.FireBackground("Error", 211, fmt.Sprintf("ListenUDP -> [%v]", err))
		return
	}
	defer conn.Close()
	mTrigger.FireBackground("Open", 100, conn)
	// Read Handler
	endC := make(chan map[int]interface{})
	msgC := make(chan interface{})
	defer close(endC)
	defer close(msgC)
	var msgBuf bytes.Buffer
	for {
		go express.brain.SafeFunction(func() {
			// Heart Config
			if err := conn.SetDeadline(heartInterval); err != nil {
				endC <- map[int]interface{}{211: fmt.Sprintf("UDPClient[SetDeadline] -> %v", err)}
				return
			}
			var udpAddr *net.UDPAddr
			// ReadBuffer
			for {
				n, addr, err := conn.ReadFromUDP(msgSlice)
				if err != nil {
					endC <- map[int]interface{}{216: fmt.Sprintf("UDPClient[Read] -> %v", err)}
					return
				}
				udpAddr = addr
				msgBuf.Write(msgSlice[:n])
				if n < bufLen {
					break
				}
			}
			msgC <- model.UDPPacket{
				Addr: udpAddr,
				Msg:  msgBuf.Bytes(),
			}
			msgBuf.Reset()
		}, func(err interface{}) {
			if err == nil {
				return
			}
			endC <- map[int]interface{}{204: fmt.Sprintf("UDPClient[Read] -> %v", err)}
		})
		select {
		case data := <-endC:
			for k, v := range data {
				mTrigger.FireBackground("Error", k, v)
			}
			return
		case data := <-msgC:
			mTrigger.FireBackground("Message", 100, data)
		}
	}
}

//* 新建串口连接 */
func (express *ExpressS) UARTClient(option serial.OpenOptions, mTrigger trigger.Trigger) {
	if express.brain.CheckIsNull(option.PortName) {
		mTrigger.FireBackground("Error", 220, "UARTClient[Option] -> Null")
		return
	}
	if express.brain.CheckIsNull(mTrigger) {
		mTrigger.FireBackground("Error", 220, "UARTClient[Trigger] -> Null")
		return
	}
	defer mTrigger.FireBackground("Close", 103, fmt.Sprintf("UARTClient[Closed] -> %v", option.PortName))
	// Initialize
	bufLen := express.brain.Const.WSParam.BufferSize
	var msgBuf bytes.Buffer
	msgSlice := make([]byte, bufLen)
	// Init Connection
	conn, err := serial.Open(option)
	if err != nil {
		mTrigger.FireBackground("Error", 222, err)
		return
	}
	defer conn.Close()
	mTrigger.FireBackground("Open", 100, conn)
	// Read Handler
	endC := make(chan map[int]interface{})
	msgC := make(chan interface{})
	defer close(endC)
	defer close(msgC)
	for {
		go express.brain.SafeFunction(func() {
			// Read Buffer
			for {
				n, err := conn.Read(msgSlice)
				if err != nil {
					endC <- map[int]interface{}{216: fmt.Sprintf("UARTClient[Read] -> %v", err)}
					return
				}
				msgBuf.Write(msgSlice[:n])
				if n < bufLen {
					break
				}
			}
			msgC <- msgBuf.Bytes()
			msgBuf.Reset()
		}, func(err interface{}) {
			if err == nil {
				return
			}
			endC <- map[int]interface{}{204: fmt.Sprintf("UARTClient[SafeFunction] -> %v", err)}
		})
		select {
		case data := <-endC:
			for k, v := range data {
				mTrigger.FireBackground("Error", k, v)
			}
			return
		case data := <-msgC:
			mTrigger.FireBackground("Message", 100, data)
		}
	}
}

//* ================================ PORT FORWARD ================================ */

//* TCP端口转发[local] */
func (express *ExpressS) TCPForward(localHost string, remoteHost string, stopC chan bool, tags ...string) {
	// Slice Paramter
	tag := "TCP2TCP"
	if len(tags) > 0 {
		tag = tags[0]
	}
	// Initial
	tag = fmt.Sprintf("%v[%v <- %v]", tag, localHost, remoteHost)
	var hub model.SyncMapHub
	hub.Init(tag)
	// Listen Local
	mTrigger := trigger.New()
	mTrigger.On("Open", func(code int, data interface{}) {
		express.brain.LogGenerater(model.LogTrace, express.tag, "TCPForward", fmt.Sprintf("Local Open -> %v", data))
	})
	mTrigger.On("Accept", func(code int, data interface{}) {
		localConn := data.(net.Conn)
		express.brain.LogGenerater(model.LogTrace, express.tag, "TCPForward", fmt.Sprintf("Local Accept -> %v", localConn.RemoteAddr()))
		// Bind Remote
		express.brain.SafeFunction(func() {
			express.tcpRemoteConn(remoteHost, localConn)
		})
	})
	mTrigger.On("Close", func(code int, data interface{}) {
		express.brain.LogGenerater(model.LogTrace, express.tag, "TCPForward", fmt.Sprintf("Local Close -> %v", data))
	})
	mTrigger.On("Error", func(code int, data interface{}) {
		express.brain.LogGenerater(model.LogTrace, express.tag, "TCPForward", fmt.Sprintf("Local Error -> %v", express.brain.MessageHandler(express.tag, "TCPForward", code, data)))
	})
	go express.TCPServer(localHost, mTrigger, stopC, hub)
}

//* UDP端口转发 */
func (express *ExpressS) UDPForward(localHost string, remoteHost string, stopC chan bool, tags ...string) {
	// Slice Paramter
	tag := "UDP2UDP"
	if len(tags) > 0 {
		tag = tags[0]
	}
	// Initial
	tag = fmt.Sprintf("%v[%v <- %v]", tag, localHost, remoteHost)
	hub := model.ConnQHub{Tag: tag, ConnQ: new(model.QueueS).New(express.brain.Const.UDPParam.MaxLen)}
	// Listen Local
	mTrigger := trigger.New()
	var remoteConn *net.UDPConn
	mTrigger.On("Open", func(code int, data interface{}) {
		localConn := data.(*net.UDPConn)
		express.brain.LogGenerater(model.LogTrace, express.tag, "UDPForward", fmt.Sprintf("Local Open -> %v", localConn.LocalAddr()))
		// Get Remote Connection
		express.brain.SafeFunction(func() {
			express.udpRemoteConn(remoteHost, localConn, hub, func(conn *net.UDPConn) {
				remoteConn = conn
			})
		})
	})
	mTrigger.On("Message", func(code int, data interface{}) {
		express.udpServerForwardMessageHandler(hub, remoteConn, data)
	})
	mTrigger.On("Close", func(code int, data interface{}) {
		express.brain.LogGenerater(model.LogTrace, express.tag, "UDPForward", fmt.Sprintf("Local Close -> %v", data))
	})
	mTrigger.On("Error", func(code int, data interface{}) {
		express.brain.LogGenerater(model.LogTrace, express.tag, "UDPForward", fmt.Sprintf("Local Error -> %v", express.brain.MessageHandler(express.tag, "UDPForward", code, data)))
	})
	go express.UDPServer(localHost, mTrigger, stopC, hub)
}

//* UDP转TCP */
func (express *ExpressS) UDP2TCPForward(localHost string, remoteHost string, stopC chan bool, tags ...string) {
	// Slice Paramter
	tag := "UDP2TCP"
	if len(tags) > 0 {
		tag = tags[0]
	}
	// Initial
	tag = fmt.Sprintf("%v[%v <- %v]", tag, localHost, remoteHost)
	var hub model.SyncMapHub
	hub.Init(tag)
	// Listen Local
	mTrigger := trigger.New()
	mTrigger.On("Open", func(code int, data interface{}) {
		express.brain.LogGenerater(model.LogTrace, express.tag, "UDP2TCPForward", fmt.Sprintf("Local Open -> %v", data))
	})
	mTrigger.On("Accept", func(code int, data interface{}) {
		localConn := data.(net.Conn)
		express.brain.LogGenerater(model.LogTrace, express.tag, "UDP2TCPForward", fmt.Sprintf("Local Accept -> %v", localConn.RemoteAddr()))
		// Bind Remote
		express.brain.SafeFunction(func() {
			express.udp2TCPRemoteConn(remoteHost, localConn)
		})
	})
	mTrigger.On("Close", func(code int, data interface{}) {
		express.brain.LogGenerater(model.LogTrace, express.tag, "UDP2TCPForward", fmt.Sprintf("Local Close -> %v", data))
	})
	mTrigger.On("Error", func(code int, data interface{}) {
		express.brain.LogGenerater(model.LogTrace, express.tag, "UDP2TCPForward", fmt.Sprintf("Local Error -> %v", express.brain.MessageHandler(express.tag, "UDP2TCPForward", code, data)))
	})
	go express.TCPServer(localHost, mTrigger, stopC, hub)
}

//* TCP转UDP */
func (express *ExpressS) TCP2UDPForward(localHost string, remoteHost string, stopC chan bool, tags ...string) {
	// Slice Paramter
	tag := "TCP2UDP"
	if len(tags) > 0 {
		tag = tags[0]
	}
	// Initial
	tag = fmt.Sprintf("%v[%v <- %v]", tag, localHost, remoteHost)
	hub := model.ConnQHub{Tag: tag, ConnQ: new(model.QueueS).New(express.brain.Const.UDPParam.MaxLen)}
	// Listen Local
	mTrigger := trigger.New()
	var remoteConn io.ReadWriteCloser
	mTrigger.On("Open", func(code int, data interface{}) {
		localConn := data.(*net.UDPConn)
		express.brain.LogGenerater(model.LogTrace, express.tag, "UART2UDPForward", fmt.Sprintf("Local Open -> %v", localConn.LocalAddr()))
		// Bind Remote
		express.brain.SafeFunction(func() {
			express.tcp2UDPRemoteConn(remoteHost, localConn, hub, func(conn net.Conn) {
				remoteConn = conn
			})
		})
	})
	mTrigger.On("Message", func(code int, data interface{}) {
		express.udpServerForwardMessageHandler(hub, remoteConn, data)
	})
	mTrigger.On("Close", func(code int, data interface{}) {
		express.brain.LogGenerater(model.LogTrace, express.tag, "UART2UDPForward", fmt.Sprintf("Local Close -> %v", data))
	})
	mTrigger.On("Error", func(code int, data interface{}) {
		express.brain.LogGenerater(model.LogTrace, express.tag, "UART2UDPForward", fmt.Sprintf("Local Error -> %v", express.brain.MessageHandler(express.tag, "UART2UDPForward", code, data)))
	})
	go express.UDPServer(localHost, mTrigger, stopC, hub)
}

//* UART转UDP */
func (express *ExpressS) UART2UDPForward(localHost string, remoteOption serial.OpenOptions, stopC chan bool, tags ...string) {
	// Slice Paramter
	tag := "UART2UDP"
	if len(tags) > 0 {
		tag = tags[0]
	}
	// Initial
	tag = fmt.Sprintf("%v[%v <- %v]", tag, localHost, remoteOption.PortName)
	hub := model.ConnQHub{Tag: tag, ConnQ: new(model.QueueS).New(express.brain.Const.UDPParam.MaxLen)}
	// Listen Local
	mTrigger := trigger.New()
	var remoteConn io.ReadWriteCloser
	mTrigger.On("Open", func(code int, data interface{}) {
		localConn := data.(*net.UDPConn)
		express.brain.LogGenerater(model.LogTrace, express.tag, "UART2UDPForward", fmt.Sprintf("Local Open -> %v", localConn.LocalAddr()))
		// Bind Remote
		express.brain.SafeFunction(func() {
			express.uart2UDPRemoteConn(remoteOption, localConn, hub, func(conn io.ReadWriteCloser) {
				remoteConn = conn
			})
		})
	})
	mTrigger.On("Message", func(code int, data interface{}) {
		express.udpServerForwardMessageHandler(hub, remoteConn, data)
	})
	mTrigger.On("Close", func(code int, data interface{}) {
		express.brain.LogGenerater(model.LogTrace, express.tag, "UART2UDPForward", fmt.Sprintf("Local Close -> %v", data))
	})
	mTrigger.On("Error", func(code int, data interface{}) {
		express.brain.LogGenerater(model.LogTrace, express.tag, "UART2UDPForward", fmt.Sprintf("Local Error -> %v", express.brain.MessageHandler(express.tag, "UART2UDPForward", code, data)))
	})
	go express.UDPServer(localHost, mTrigger, stopC, hub)
}
