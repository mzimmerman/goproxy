package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"

	"github.com/elazarl/goproxy"
	"github.com/inconshreveable/go-vhost"
)

func orPanic(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	verbose := flag.Bool("v", false, "should every proxy request be logged to stdout")
	http_addr := flag.String("httpaddr", ":3129", "proxy http listen address")
	https_addr := flag.String("httpsaddr", ":3128", "proxy https listen address")
	flag.Parse()

	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = *verbose
	proxy.NonproxyHandler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Host == "" {
			fmt.Fprintln(w, "Cannot handle requests without Host header, e.g., HTTP 1.0")
			return
		}
		req.URL.Scheme = "http"
		req.URL.Host = req.Host
		proxy.ServeHTTP(w, req)
	})
	proxy.OnRequest().
		HijackConnect(func(req *http.Request, client net.Conn, ctx *goproxy.ProxyCtx) {
		defer func() {
			if e := recover(); e != nil {
				ctx.Logf("error connecting to remote: %v", e)
				client.Write([]byte("HTTP/1.1 500 Cannot reach destination\r\n\r\n"))
			}
			client.Close()
		}()
		clientBuf := bufio.NewReadWriter(bufio.NewReader(client), bufio.NewWriter(client))
		remote, err := net.Dial("tcp", req.URL.Host)
		orPanic(err)
		remoteBuf := bufio.NewReadWriter(bufio.NewReader(remote), bufio.NewWriter(remote))
		for {
			req, err := http.ReadRequest(clientBuf.Reader)
			orPanic(err)
			orPanic(req.Write(remoteBuf))
			orPanic(remoteBuf.Flush())
			resp, err := http.ReadResponse(remoteBuf.Reader, req)
			orPanic(err)
			orPanic(resp.Write(clientBuf.Writer))
			orPanic(clientBuf.Flush())
		}
	})
	go func() {
		log.Fatalln(http.ListenAndServe(*http_addr, proxy))
	}()

	//	func (pcond *ReqProxyConds) HijackConnect(f func(req *http.Request, client net.Conn, ctx *ProxyCtx)) {
	//	pcond.proxy.httpsHandlers = append(pcond.proxy.httpsHandlers,
	//		FuncHttpsHandler(func(host string, ctx *ProxyCtx) (*ConnectAction, string) {
	//			for _, cond := range pcond.reqConds {
	//				if !cond.HandleReq(ctx.Req, ctx) {
	//					return nil, ""
	//				}
	//			}
	//			return &ConnectAction{Action: ConnectHijack, Hijack: f}, host
	//		}))
	//}

	// now start the TLS handshake but tear down the connection enough to make it a CONNECT request instead
	ln, err := net.Listen("tcp", *https_addr)
	if err != nil {
		log.Fatalf("Error listening for https connections - %v", err)
	}
	for {
		c, err := ln.Accept()
		if err != nil {
			log.Printf("Error accepting new connection - %v", err)
			continue
		}
		go func(c net.Conn) {
			tlsConn, err := vhost.TLS(c)
			if err != nil {
				log.Printf("Error accepting new connection - %v", err)
			}
			if tlsConn.Host() == "" {
				log.Printf("Cannot handle non-SNI enabled clients")
				return
			}
			proxy.HandleHttpsConn(tlsConn, tlsConn.Host())
		}(c)
	}
}
