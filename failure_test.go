package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// TLS のレコードの形だけを整えた ClientHello。サーバーはレコード 1 つ分を読むだけで中身は解釈しない
var fakeClientHello = []byte{0x16, 0x03, 0x01, 0x00, 0x04, 0x01, 0x00, 0x00, 0x00}

var closeNotifyAlert = []byte{0x15, 0x03, 0x03, 0x00, 0x02, 0x01, 0x00}

// startEcho は newEcho のハンドラーを、本番の起動と同じく接続ごとの要求の数え上げ付きで起動する
func startEcho(t *testing.T, useTLS bool) *httptest.Server {
	t.Helper()
	ts := httptest.NewUnstartedServer(newEcho())
	ts.Config.ConnContext = connContext
	if useTLS {
		ts.StartTLS()
	} else {
		ts.Start()
	}
	t.Cleanup(ts.Close)
	return ts
}

func startListener(t *testing.T, serve func(net.Listener)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go serve(ln)
	t.Cleanup(func() { ln.Close() })
	return ln.Addr().String()
}

func dialRaw(t *testing.T, addr string) net.Conn {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c.SetDeadline(time.Now().Add(5 * time.Second))
	t.Cleanup(func() { c.Close() })
	return c
}

func dialTLS(t *testing.T, addr string) *tls.Conn {
	t.Helper()
	c, err := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", addr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	c.SetDeadline(time.Now().Add(5 * time.Second))
	t.Cleanup(func() { c.Close() })
	return c
}

func writeRequest(t *testing.T, w io.Writer, method, path string, body []byte) {
	t.Helper()
	head := fmt.Sprintf("%s %s HTTP/1.1\r\nHost: localhost\r\nContent-Length: %d\r\n\r\n", method, path, len(body))
	if _, err := w.Write(append([]byte(head), body...)); err != nil {
		t.Fatal(err)
	}
}

// readUntilClosed は相手が閉じるまで読み、読めたバイトと閉じたときのエラーを返す。
// io.Copy は EOF を成功として扱い RST と区別できないので使わない
func readUntilClosed(r io.Reader) ([]byte, error) {
	var out []byte
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			return out, err
		}
	}
}

func assertEOF(t *testing.T, got []byte, err error) {
	t.Helper()
	if !errors.Is(err, io.EOF) || len(got) != 0 {
		t.Fatalf("何も返さずに閉じる（EOF）はず: got=%q err=%v", got, err)
	}
}

func assertReset(t *testing.T, got []byte, err error) {
	t.Helper()
	if !errors.Is(err, syscall.ECONNRESET) || len(got) != 0 {
		t.Fatalf("何も返さずに RST で切るはず: got=%q err=%v", got, err)
	}
}

func assertCloseNotify(t *testing.T, got []byte, err error) {
	t.Helper()
	if !bytes.Equal(got, closeNotifyAlert) || !errors.Is(err, io.EOF) {
		t.Fatalf("close_notify のアラートだけを返して閉じるはず: got=%x err=%v", got, err)
	}
}

func readResponse(t *testing.T, br *bufio.Reader, req *http.Request) (*http.Response, string) {
	t.Helper()
	res, err := http.ReadResponse(br, req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return res, string(body)
}

func TestClose(t *testing.T) {
	ts := startEcho(t, false)
	addr := ts.Listener.Addr().String()

	t.Run("eof は応答せずに閉じる", func(t *testing.T) {
		c := dialRaw(t, addr)
		writeRequest(t, c, "GET", "/close/eof", nil)
		got, err := readUntilClosed(c)
		assertEOF(t, got, err)
	})

	t.Run("reset は応答せずに RST で切る", func(t *testing.T) {
		c := dialRaw(t, addr)
		writeRequest(t, c, "GET", "/close/reset", nil)
		got, err := readUntilClosed(c)
		assertReset(t, got, err)
	})

	t.Run("本文を読み終えてから閉じる", func(t *testing.T) {
		// 読み残しがあると OS は FIN ではなく RST を送る。EOF で閉じられれば読み終えている
		c := dialRaw(t, addr)
		writeRequest(t, c, "POST", "/close/eof", bytes.Repeat([]byte("a"), 256*1024))
		got, err := readUntilClosed(c)
		assertEOF(t, got, err)
	})

	t.Run("知らない mode は 400", func(t *testing.T) {
		res, err := http.Get(ts.URL + "/close/unknown")
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("status=%d", res.StatusCode)
		}
	})
}

func TestCloseOverTLS(t *testing.T) {
	ts := startEcho(t, true)
	addr := ts.Listener.Addr().String()

	t.Run("eof", func(t *testing.T) {
		c := dialTLS(t, addr)
		writeRequest(t, c, "GET", "/close/eof", nil)
		got, err := readUntilClosed(c)
		assertEOF(t, got, err)
	})

	t.Run("reset", func(t *testing.T) {
		c := dialTLS(t, addr)
		writeRequest(t, c, "GET", "/close/reset", nil)
		got, err := readUntilClosed(c)
		assertReset(t, got, err)
	})
}

func TestKeepAlive(t *testing.T) {
	ts := startEcho(t, false)
	addr := ts.Listener.Addr().String()

	t.Run("同じ接続の 2 本目を読んでから閉じる", func(t *testing.T) {
		c := dialRaw(t, addr)
		br := bufio.NewReader(c)

		writeRequest(t, c, "GET", "/keepalive/eof", nil)
		res, body := readResponse(t, br, nil)
		if res.StatusCode != http.StatusOK || !strings.Contains(body, "request 1 ") {
			t.Fatalf("1 本目は 200 で応答するはず: status=%d body=%q", res.StatusCode, body)
		}

		writeRequest(t, c, "GET", "/keepalive/eof", nil)
		got, err := readUntilClosed(br)
		assertEOF(t, got, err)
	})

	t.Run("nth で何本目に閉じるかを決められる", func(t *testing.T) {
		c := dialRaw(t, addr)
		br := bufio.NewReader(c)

		for i := 1; i <= 2; i++ {
			writeRequest(t, c, "GET", "/keepalive/reset?nth=3", nil)
			res, body := readResponse(t, br, nil)
			if res.StatusCode != http.StatusOK || !strings.Contains(body, fmt.Sprintf("request %d ", i)) {
				t.Fatalf("%d 本目は 200 で応答するはず: status=%d body=%q", i, res.StatusCode, body)
			}
		}

		writeRequest(t, c, "GET", "/keepalive/reset?nth=3", nil)
		got, err := readUntilClosed(br)
		assertReset(t, got, err)
	})

	t.Run("ほかのパスへの要求も同じ接続の本数に数える", func(t *testing.T) {
		c := dialRaw(t, addr)
		br := bufio.NewReader(c)

		writeRequest(t, c, "GET", "/nothing", nil)
		readResponse(t, br, nil)

		writeRequest(t, c, "GET", "/keepalive/eof", nil)
		got, err := readUntilClosed(br)
		assertEOF(t, got, err)
	})

	t.Run("Go の Transport は 2 本目の GET を新しい接続で送り直し、POST は送り直さない", func(t *testing.T) {
		newClient := func(dials *atomic.Int64) *http.Client {
			tr := &http.Transport{DialContext: func(ctx context.Context, network, a string) (net.Conn, error) {
				dials.Add(1)
				return (&net.Dialer{}).DialContext(ctx, network, a)
			}}
			t.Cleanup(tr.CloseIdleConnections)
			return &http.Client{Transport: tr, Timeout: 5 * time.Second}
		}

		var getDials atomic.Int64
		getClient := newClient(&getDials)
		for i := 1; i <= 2; i++ {
			res, err := getClient.Get(ts.URL + "/keepalive/eof")
			if err != nil {
				t.Fatalf("%d 本目の GET: %v", i, err)
			}
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
		}
		if got := getDials.Load(); got != 2 {
			t.Fatalf("2 本目は新しい接続で送り直すはず: dials=%d", got)
		}

		var postDials atomic.Int64
		postClient := newClient(&postDials)
		post := func() error {
			res, err := postClient.Post(ts.URL+"/keepalive/eof", "text/plain", strings.NewReader("x"))
			if err != nil {
				return err
			}
			io.Copy(io.Discard, res.Body)
			return res.Body.Close()
		}
		if err := post(); err != nil {
			t.Fatalf("1 本目の POST: %v", err)
		}
		if err := post(); err == nil {
			t.Fatal("2 本目の POST は送り直されずに失敗するはず")
		}
	})
}

func TestTLSFail(t *testing.T) {
	cases := []struct {
		mode  string
		check func(*testing.T, []byte, error)
	}{
		{"close_notify", assertCloseNotify},
		{"reset", assertReset},
		{"eof", assertEOF},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			addr := startListener(t, func(ln net.Listener) { serveTLSFail(ln, tc.mode) })
			c := dialRaw(t, addr)
			if _, err := c.Write(fakeClientHello); err != nil {
				t.Fatal(err)
			}
			got, err := readUntilClosed(c)
			tc.check(t, got, err)
		})
	}

	t.Run("本物の TLS クライアントはハンドシェイクに失敗する", func(t *testing.T) {
		addr := startListener(t, func(ln net.Listener) { serveTLSFail(ln, "close_notify") })
		c, err := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", addr, &tls.Config{InsecureSkipVerify: true})
		if err == nil {
			c.Close()
			t.Fatal("ハンドシェイクは失敗するはず")
		}
		// サーバーが何もしなくてもタイムアウトで失敗になるので、タイムアウトでないことまで見る
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			t.Fatalf("タイムアウトではなく、サーバーに切られて失敗するはず: %v", err)
		}
	})
}

// connect は CONNECT を送ってプロキシの応答のヘッダーを読む。
// CONNECT への 2xx は本文を持たず、その後はトンネルになる（RFC 9110 9.3.6）ので、本文は読まない
func connect(t *testing.T, proxyAddr, target string) (net.Conn, *bufio.Reader, *http.Response) {
	t.Helper()
	c := dialRaw(t, proxyAddr)
	fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	br := bufio.NewReader(c)
	res, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	return c, br, res
}

func TestProxy(t *testing.T) {
	t.Run("503 は CONNECT に 503 を返す", func(t *testing.T) {
		proxyAddr := startListener(t, func(ln net.Listener) { serveProxy(ln, "503") })
		_, _, res := connect(t, proxyAddr, "example.com:443")
		if res.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("status=%d", res.StatusCode)
		}
	})

	cases := []struct {
		mode  string
		check func(*testing.T, []byte, error)
	}{
		{"close_notify", assertCloseNotify},
		{"reset", assertReset},
		{"eof", assertEOF},
	}
	for _, tc := range cases {
		t.Run(tc.mode+" は CONNECT に 200 を返してからハンドシェイクで切る", func(t *testing.T) {
			proxyAddr := startListener(t, func(ln net.Listener) { serveProxy(ln, tc.mode) })
			c, br, res := connect(t, proxyAddr, "example.com:443")
			if res.StatusCode != http.StatusOK {
				t.Fatalf("status=%d", res.StatusCode)
			}
			if _, err := c.Write(fakeClientHello); err != nil {
				t.Fatal(err)
			}
			got, err := readUntilClosed(br)
			tc.check(t, got, err)
		})
	}

	t.Run("tunnel は宛先へ中継する", func(t *testing.T) {
		ts := startEcho(t, true)
		proxyAddr := startListener(t, func(ln net.Listener) { serveProxy(ln, "tunnel") })
		tr := ts.Client().Transport.(*http.Transport).Clone()
		tr.Proxy = http.ProxyURL(&url.URL{Scheme: "http", Host: proxyAddr})
		t.Cleanup(tr.CloseIdleConnections)
		client := &http.Client{Transport: tr, Timeout: 5 * time.Second}

		res, err := client.Get(ts.URL + "/statuscode/204")
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNoContent {
			t.Fatalf("status=%d", res.StatusCode)
		}
	})

	t.Run("tunnel で宛先につながらなければ 503", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		closedAddr := ln.Addr().String()
		ln.Close()

		proxyAddr := startListener(t, func(ln net.Listener) { serveProxy(ln, "tunnel") })
		_, _, res := connect(t, proxyAddr, closedAddr)
		if res.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("status=%d", res.StatusCode)
		}
	})

	t.Run("CONNECT 以外は 405", func(t *testing.T) {
		proxyAddr := startListener(t, func(ln net.Listener) { serveProxy(ln, "tunnel") })
		c := dialRaw(t, proxyAddr)
		fmt.Fprint(c, "GET http://example.com/ HTTP/1.1\r\nHost: example.com\r\n\r\n")
		res, _ := readResponse(t, bufio.NewReader(c), nil)
		if res.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("status=%d", res.StatusCode)
		}
	})
}
