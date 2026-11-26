package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"slices"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/labstack/echo/v5"
)

// 接続の切り方と、偽プロキシの振る舞い
const (
	modeEOF         = "eof"          // 応答せずに FIN で閉じる
	modeReset       = "reset"        // 応答せずに RST で切る
	modeCloseNotify = "close_notify" // TLS の close_notify のアラートだけを返して閉じる
	modeTunnel      = "tunnel"       // 宛先へ中継する
	mode503         = "503"          // CONNECT に 503 を返す
)

var (
	closeModes   = []string{modeEOF, modeReset}
	tlsFailModes = []string{modeCloseNotify, modeReset, modeEOF}
	proxyModes   = []string{modeTunnel, mode503, modeCloseNotify, modeReset, modeEOF}
)

// レベル warning・内容 close_notify の TLS 1.2 のアラートのレコード。
// ハンドシェイク中にこれを受けると、OpenSSL は SSL_ERROR_ZERO_RETURN（SSL_connect returned=6）を返す
var closeNotifyRecord = []byte{0x15, 0x03, 0x03, 0x00, 0x02, 0x01, 0x00}

const connectEstablished = "HTTP/1.1 200 Connection established\r\n\r\n"

type connCounterKey struct{}

const requestNoKey = "connRequestNo"

// connContext は接続ごとの要求の数を context に持たせる。http.Server.ConnContext に渡す
func connContext(ctx context.Context, _ net.Conn) context.Context {
	return context.WithValue(ctx, connCounterKey{}, new(atomic.Int64))
}

// countRequestsPerConn は同じ接続の何本目の要求かを数える。
// keepalive がパスを問わず本数を数えられるよう、ルーティングの前（Pre）で使う
func countRequestsPerConn(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if counter, ok := c.Request().Context().Value(connCounterKey{}).(*atomic.Int64); ok {
			c.Set(requestNoKey, counter.Add(1))
		}
		return next(c)
	}
}

func closeHandler(c *echo.Context) error {
	mode := c.Param("mode")
	if !slices.Contains(closeModes, mode) {
		return c.String(http.StatusBadRequest, fmt.Sprintf("mode は %v のどれか\n", closeModes))
	}
	return closeWithoutResponse(c, mode)
}

func keepAliveHandler(c *echo.Context) error {
	mode := c.Param("mode")
	if !slices.Contains(closeModes, mode) {
		return c.String(http.StatusBadRequest, fmt.Sprintf("mode は %v のどれか\n", closeModes))
	}
	nth := 2
	if q := c.QueryParam("nth"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 1 {
			return c.String(http.StatusBadRequest, "nth は 1 以上の整数\n")
		}
		nth = n
	}

	no, _ := c.Get(requestNoKey).(int64)
	if no < int64(nth) {
		return c.String(http.StatusOK, fmt.Sprintf("request %d on this connection (closes at %d)\n", no, nth))
	}
	return closeWithoutResponse(c, mode)
}

// closeWithoutResponse は要求を本文まで読んでから、応答せずに接続を閉じる
func closeWithoutResponse(c *echo.Context, mode string) error {
	// 読み残しがあると、eof でも OS が FIN ではなく RST を送ってしまう
	if _, err := io.Copy(io.Discard, c.Request().Body); err != nil {
		return err
	}
	conn, _, err := http.NewResponseController(c.Response()).Hijack()
	if err != nil {
		return err
	}
	log.Printf("close: %s %s %s from %s", mode, c.Request().Method, c.Request().URL, c.Request().RemoteAddr)
	// 乗っ取った後にエラーを返すと echo が応答を書こうとするので、ログに残すだけにする
	if err := closeRaw(conn, mode); err != nil {
		log.Printf("close: %v", err)
	}
	return nil
}

// closeRaw は TLS の上でも close_notify を送らず、下の TCP をそのまま閉じる。
// 相手が落ちたときと同じ見え方にするため
func closeRaw(conn net.Conn, mode string) error {
	if tc, ok := conn.(*tls.Conn); ok {
		conn = tc.NetConn()
	}
	if mode == modeReset {
		if tcp, ok := conn.(*net.TCPConn); ok {
			// 0 秒にすると、Close で FIN ではなく RST を送る
			if err := tcp.SetLinger(0); err != nil {
				return err
			}
		}
	}
	return conn.Close()
}

// serveTLSFail は、ClientHello を受けたところで mode のとおりに接続を切るサーバー
func serveTLSFail(ln net.Listener, mode string) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			failHandshake(conn, conn, mode)
		}()
	}
}

// failHandshake は ClientHello のレコードを 1 つ読み終えてから、mode のとおりに切る。
// r は conn の読み口。プロキシでは CONNECT を読んだ bufio.Reader に残りがあるので、それを渡す
func failHandshake(conn net.Conn, r io.Reader, mode string) {
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	if err := readTLSRecord(r); err != nil {
		log.Printf("tlsfail: ClientHello を読めなかった from %s: %v", conn.RemoteAddr(), err)
		return
	}
	log.Printf("tlsfail: %s from %s", mode, conn.RemoteAddr())
	if mode == modeCloseNotify {
		if _, err := conn.Write(closeNotifyRecord); err != nil {
			log.Printf("tlsfail: %v", err)
			return
		}
	}
	if err := closeRaw(conn, mode); err != nil {
		log.Printf("tlsfail: %v", err)
	}
}

// readTLSRecord はレコードのヘッダー（5 バイト）にある長さの分だけ読み捨てる。
// ClientHello は TCP の複数の区切りに分かれて届くことがあるので、1 回の Read では足りない
func readTLSRecord(r io.Reader) error {
	header := make([]byte, 5)
	if _, err := io.ReadFull(r, header); err != nil {
		return err
	}
	length := int64(header[3])<<8 | int64(header[4])
	_, err := io.CopyN(io.Discard, r, length)
	return err
}

// serveProxy は CONNECT だけを受ける HTTP プロキシ。CONNECT の後の振る舞いを mode で変える
func serveProxy(ln net.Listener, mode string) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go handleProxyConn(conn, mode)
	}
}

func handleProxyConn(conn net.Conn, mode string) {
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReader(conn)
	req, err := http.ReadRequest(br)
	if err != nil {
		log.Printf("proxy: 要求を読めなかった from %s: %v", conn.RemoteAddr(), err)
		return
	}
	if req.Method != http.MethodConnect {
		writeProxyStatus(conn, http.StatusMethodNotAllowed)
		return
	}
	target := req.URL.Host
	log.Printf("proxy: %s CONNECT %s from %s", mode, target, conn.RemoteAddr())

	switch mode {
	case mode503:
		writeProxyStatus(conn, http.StatusServiceUnavailable)
	case modeTunnel:
		tunnel(conn, br, target)
	default:
		if _, err := io.WriteString(conn, connectEstablished); err != nil {
			return
		}
		failHandshake(conn, br, mode)
	}
}

func writeProxyStatus(w io.Writer, code int) {
	fmt.Fprintf(w, "HTTP/1.1 %d %s\r\nContent-Length: 0\r\nConnection: close\r\n\r\n", code, http.StatusText(code))
}

// tunnel は宛先につないで中継する。宛先につながらなければ、squid と同じく CONNECT に 503 を返す。
// どちらかの向きが終わったら両方を閉じる
func tunnel(conn net.Conn, br *bufio.Reader, target string) {
	upstream, err := net.DialTimeout("tcp", target, 10*time.Second)
	if err != nil {
		log.Printf("proxy: %s につながらない: %v", target, err)
		writeProxyStatus(conn, http.StatusServiceUnavailable)
		return
	}
	defer upstream.Close()
	if _, err := io.WriteString(conn, connectEstablished); err != nil {
		return
	}
	conn.SetReadDeadline(time.Time{})

	done := make(chan struct{}, 2)
	go func() {
		// クライアントが CONNECT の直後に送ったバイトは br に入っているので、br から読む
		io.Copy(upstream, br)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(conn, upstream)
		done <- struct{}{}
	}()
	<-done
}

// startRawServer は TLS 失敗用と偽プロキシ用のポートを開く。port が 0 なら何もしない
func startRawServer(name string, port int, mode string, modes []string, serve func(net.Listener, string)) {
	if port == 0 {
		return
	}
	if !slices.Contains(modes, mode) {
		log.Fatalf("%s: mode は %v のどれか: %q", name, modes, mode)
	}
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("%s: listening on %s (mode=%s)", name, ln.Addr(), mode)
	go serve(ln, mode)
}
