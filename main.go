package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

// -days を省くと有効期限が 30 日になる。
// keyCertSign がないと、OpenSSL（Ruby など）はこの証明書を ca_file に渡しても自分自身の発行者と認めず、検証に失敗する（Go は通る）
// openssl req -x509 -days 3650 -out localhost.crt -keyout localhost.key -newkey rsa:2048 -nodes -sha256 -subj '/CN=localhost' -extensions EXT -config <(printf "[dn]\nCN=localhost\n[req]\ndistinguished_name = dn\n[EXT]\nsubjectAltName=DNS:localhost,IP:127.0.0.1\nkeyUsage=digitalSignature,keyCertSign\nextendedKeyUsage=serverAuth")
var localhostCert = []byte(`-----BEGIN CERTIFICATE-----
MIIC9jCCAd6gAwIBAgIUX0+NJ6lHgQUT7J5ZeJJDdACF/LkwDQYJKoZIhvcNAQEL
BQAwFDESMBAGA1UEAwwJbG9jYWxob3N0MB4XDTI2MTEyNjAxNTEyMFoXDTM2MTEy
MzAxNTEyMFowFDESMBAGA1UEAwwJbG9jYWxob3N0MIIBIjANBgkqhkiG9w0BAQEF
AAOCAQ8AMIIBCgKCAQEAxELXHIJTkqw5/h5ckrb3MGwF/r94D35BLxmQdDkJ5t/H
BN9AGcoKmbsrekxy51XySZ4NK4Pxmk4v7ncyh0RAR8PQeuBUOm6yQ1EJ3ZVPrVgn
HQ60Wfp+5WrGN8Gg/vTdL7UbB7vlSYkuUOox6mjVRPg62qr34ziu30LgsXGUQ2CI
j4rQVQXQq0x/hcEqr3Q+YdzwQU8wPkLL0gJgueyuruH7gojgZjobK04EuKc/74S6
inqri73i2sRfVavmSFO8uWsmqzsX/7l7fgo3/5zJ8XQkxYRNqYlVssaCkHo5qevv
MxdiWCBWmuylTPSwC6EJdkvNia7He1kNaI23nicWuQIDAQABo0AwPjAaBgNVHREE
EzARgglsb2NhbGhvc3SHBH8AAAEwCwYDVR0PBAQDAgKEMBMGA1UdJQQMMAoGCCsG
AQUFBwMBMA0GCSqGSIb3DQEBCwUAA4IBAQChSULm573zcEMp+fPZDSHwJlhopiGa
yWg0Oaq3KA/s3wJirsrq2acHiJ5Wee0bHm8XWSj/52ImCdky4EN8NKgJJ2yPfrVi
1IS8aQ6bxo4fCOb4u2xW16oBJG9NejixlK5cvEzJt0+mCCsSnecDUGWxpnkj6jwK
cCH5WahIX1eXE/7xFVGtMyq61ubBLJGI5mn6m55dnSrtw/g4pwmgZrXPlJg5FESR
nGlFfQVwfHUT4XooHiftOGhG66KN9+ovFmtAZpf+QfKXsrnV4PWZ1s8V1b69JTIg
MBU+g9mvlya6r6Aof5FdQy7bPpXSgVhuswv0G9gKMIImdqH96zSiRBv3
-----END CERTIFICATE-----`)

var localhostKey = []byte(`-----BEGIN PRIVATE KEY-----
MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQDEQtccglOSrDn+
HlyStvcwbAX+v3gPfkEvGZB0OQnm38cE30AZygqZuyt6THLnVfJJng0rg/GaTi/u
dzKHREBHw9B64FQ6brJDUQndlU+tWCcdDrRZ+n7lasY3waD+9N0vtRsHu+VJiS5Q
6jHqaNVE+DraqvfjOK7fQuCxcZRDYIiPitBVBdCrTH+FwSqvdD5h3PBBTzA+QsvS
AmC57K6u4fuCiOBmOhsrTgS4pz/vhLqKequLveLaxF9Vq+ZIU7y5ayarOxf/uXt+
Cjf/nMnxdCTFhE2piVWyxoKQejmp6+8zF2JYIFaa7KVM9LALoQl2S82Jrsd7WQ1o
jbeeJxa5AgMBAAECggEAGtEzSX75YnJMHlARwC5mCmTIUO3ivuM2jvQETA07MCPl
suG6hYuY9TahEz38+WVCDRMeo5KM4dPLMt9wtRD7Y5L7Gr8qYy2bCB27pl5vm7UO
6yaGOEFFM53qZdX8ZLoauxkZD256PxkSjhXiKjCW66yrhhOUeaTWKEWgJD7rzmTW
Bd7OVgHY1ayyA6S4zPZjKLGbcqBIIAnXQZOZVovCPkDtuSrtDhyLWLyWrhzLVrir
xnaNn0FiXYC+fKhW9gyhAU8uX4kRmYa1fD053aEORgInLm2YVa326CMECBW4nYYN
I3sKkVQeM+W9kF78cB+Eai8OYJQYS7VyiqDanoHqcQKBgQDuF+JJ/SC/DSqmh17R
L6WljDP/g+o/GCv01Q991NjmURQYUvfnRLgTDMN9hDvhmFmFsqmH4+rJQCTrJZYT
Vl+0+UYEJCi0OHjfM+npxvla+k4fJMZY2TzsxwMNQOK8Y2gFSIFVftclB7C/flni
rqZY+uiBk4pIG1vHbOXEmH3C3QKBgQDTBYrYXaoKV3L1fFGy9Uu4cy7eRAUfy1ND
3ZBgavpRPhE9bvRdeLX9kAGce3ldChF9/osgWEtZcV4/D51sw0DQfjBLA/Ucx2Lx
kTPiiWZm9YkSY8zUGhLutP4HawrvC9W9j+bdhq1n5AYGn0bgBxNQ+ucY25JG1bDh
d7bHcdIfjQKBgFcBeVsKWkYWvKhAPcD5xXGaq4HLJfXKkn2UwmV8O4rmxA4je54S
uaWUe3fJs8w7zTMTV+plum2yuAsKIC6O2Miccx6eTBWXsWv3X5pjcMnzQEsXODhN
onrwOshEFtc7CE8t8L1abaqFhexkctpocjd/8Skwzk6NWNumisxt4T8tAoGAYs8C
j4N0HnmKI7Z9jt1bAlg0xvB0scmECJATjYIqCaaVbf2pQKdIGB25E2uhjVYskKGC
xbdyOMRjzAVkwku+6qFbvZRxznLyOoiyEc4ie+1WrJy8iUhYOxvWouSVbw9MmwFp
qarpQW88/xbPmhDWE6IYWcRefvqKY1ip5kO+jvUCgYEAnouLtITHa0pD28qd0oVu
rwFtQx1DhhKNo3w1xYBOG2RC6iPCHL285wjWzVAvv5WNh4KqOtwyokBdx7mqIPzU
r+SqDCaV+SyAPxs/DgcPjmrZDAAj0OffwW+/qCv0dcY2HVj8OkxQaXOWsF3lkbWY
dMeLXylc3OJdkaZQiKgfBLk=
-----END PRIVATE KEY-----`)

var (
	ssl                   *bool
	port                  *int
	readTimeoutMsec       *int
	writeTimeoutMsec      *int
	readHeaderTimeoutMsec *int
	idleTimeoutMsec       *int
	gzipEnable            *bool
	tlsConfig             *tls.Config
)

func main() {
	ssl = flag.Bool("s", false, "ssl")
	port = flag.Int("p", 3001, "binding port")
	readTimeoutMsec = flag.Int("rt", 0, "read timeout milliseconds")
	readHeaderTimeoutMsec = flag.Int("rht", 0, "read header timeout milliseconds")
	writeTimeoutMsec = flag.Int("wt", 0, "write timeout milliseconds")
	idleTimeoutMsec = flag.Int("it", 0, "idle timeout milliseconds")
	gzipEnable = flag.Bool("gz", false, "gzip")
	flag.Parse()

	e := echo.New()
	e.Use(middleware.RequestLogger())
	e.Use(middleware.Recover())
	// v5 の CORS() は引数なしだと設定エラーになる。v4 の既定と同じく全オリジンを許可する
	e.Use(middleware.CORS("*"))
	e.Use(middleware.BodyDump(func(c *echo.Context, reqBody, resBody []byte, err error) {
		log.Println(string(reqBody))
	}))
	if *gzipEnable {
		e.Use(middleware.GzipWithConfig(middleware.GzipConfig{}))
	}

	e.Any("/", dump)
	e.Any("/dump", dump)
	e.GET("/help", help)
	e.GET("/nothing", nothing)
	e.GET("/streaming", streaming)
	e.GET("/streaming/infinite", streamingInfinite)
	e.GET("/301redirect", movedPermanently)
	e.GET("/302redirect", found)
	e.GET("/sleep", sleep)
	e.GET("/json/valid", validjson)
	e.GET("/json/invalid", invalidjson)
	e.GET("/invalidlength/long", invalidlengthTooLong)
	e.GET("/invalidlength/short", invalidlengthTooShort)
	e.GET("/statuscode/:code", statuscode)
	e.GET("/nabeatsu/:num", nabeatsu)

	if *ssl {
		cert, err := tls.X509KeyPair(localhostCert, localhostKey)
		if err != nil {
			log.Println(err)
			return
		}
		tlsConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
	}

	sc := echo.StartConfig{
		Address: fmt.Sprintf(":%d", *port),
		// StartTLS は h2 を有効にするので使わない。v4 の StartServer と同じく HTTP/1.1 で TLS を待つ
		TLSConfig: tlsConfig,
		BeforeServeFunc: func(s *http.Server) error {
			// v5 は ReadTimeout を 30 秒にするので、フラグの値（既定は 0 = 無制限）で上書きする
			s.ReadTimeout = time.Duration(*readTimeoutMsec) * time.Millisecond
			s.ReadHeaderTimeout = time.Duration(*readHeaderTimeoutMsec) * time.Millisecond
			s.WriteTimeout = time.Duration(*writeTimeoutMsec) * time.Millisecond
			s.IdleTimeout = time.Duration(*idleTimeoutMsec) * time.Millisecond
			log.Println(s)
			return nil
		},
	}

	if err := sc.Start(context.Background(), e); err != nil {
		log.Fatal(err)
	}
}

func dump(c *echo.Context) error {
	req := c.Request()

	var ret []string

	ret = append(ret, fmt.Sprintf("%s %s %s", req.Method, req.URL.Path, req.Proto))
	ret = append(ret, fmt.Sprintf("Host: %s", req.Host))

	for k, values := range req.Header {
		for _, v := range values {
			ret = append(ret, fmt.Sprintf("%s: %s", k, v))
		}
	}

	buf := new(bytes.Buffer)
	buf.ReadFrom(req.Body)
	ret = append(ret, buf.String())

	return c.String(http.StatusOK, strings.Join(ret, "\n"))
}

func hello(c *echo.Context) error {
	return c.String(http.StatusOK, "Hello, World!\n")
}

func help(c *echo.Context) error {
	msg := `
curl http://localhost:{{.port}}/
curl http://localhost:{{.port}}/dump
curl http://localhost:{{.port}}/nothing
curl http://localhost:{{.port}}/streaming
curl http://localhost:{{.port}}/streaming/infinite
curl http://localhost:{{.port}}/301redirect?url=http://example.com
curl http://localhost:{{.port}}/302redirect?url=http://example.com
curl http://localhost:{{.port}}/sleep?msec=1000
curl http://localhost:{{.port}}/json/valid
curl http://localhost:{{.port}}/json/invalid
curl http://localhost:{{.port}}/invalidlength/long
curl http://localhost:{{.port}}/invalidlength/short
curl http://localhost:{{.port}}/statuscode/:code
  `

	t := template.Must(template.New("name").Parse(msg))
	buf := new(bytes.Buffer)
	data := map[string]interface{}{"port": *port}
	t.Execute(buf, data)

	return c.String(http.StatusOK, buf.String())
}

func nothing(c *echo.Context) error {
	return c.String(http.StatusOK, "")
}

func streaming(c *echo.Context) error {
	c.Response().WriteHeader(http.StatusOK)
	cnt := 5
	for i := 1; i <= cnt; i++ {
		m := fmt.Sprintf("Streaming %d/%d\n", i, cnt)
		_, err := c.Response().Write([]byte(m))
		if err != nil {
			return err
		}
		if err := http.NewResponseController(c.Response()).Flush(); err != nil {
			return err
		}
		time.Sleep(1 * time.Second)
	}
	return nil
}

func streamingInfinite(c *echo.Context) error {
	c.Response().WriteHeader(http.StatusOK)
	for {
		_, err := c.Response().Write([]byte("HELLO\n"))
		if err != nil {
			return err
		}
		if err := http.NewResponseController(c.Response()).Flush(); err != nil {
			return err
		}
		time.Sleep(1 * time.Second)
	}
}

func movedPermanently(c *echo.Context) error {
	url := c.QueryParam("url")
	return c.Redirect(http.StatusMovedPermanently, url)
}

func found(c *echo.Context) error {
	url := c.QueryParam("url")
	return c.Redirect(http.StatusFound, url)
}

func sleep(c *echo.Context) error {
	msec := c.QueryParam("msec")
	i, err := strconv.Atoi(msec)
	if err != nil {
		i = 0
	}

	time.Sleep(time.Millisecond * time.Duration(i))

	return c.String(http.StatusOK, fmt.Sprintf("Sleep %d milliseconds.", i))
}

func validjson(c *echo.Context) error {
	c.Response().Header().Set(echo.HeaderContentType, echo.MIMEApplicationJSONCharsetUTF8)
	return c.String(http.StatusOK, "{\"name\":\"hello\"}")
}

func invalidjson(c *echo.Context) error {
	c.Response().Header().Set(echo.HeaderContentType, echo.MIMEApplicationJSONCharsetUTF8)
	return c.String(http.StatusOK, "{\"name\":\"hello")
}

func invalidlengthTooShort(c *echo.Context) error {
	c.Response().Header().Set(echo.HeaderContentLength, "100")
	return c.String(http.StatusOK, "Hello")
}

func invalidlengthTooLong(c *echo.Context) error {
	c.Response().Header().Set(echo.HeaderContentLength, "1")
	return c.String(http.StatusOK, "Hello")
}

func statuscode(c *echo.Context) error {
	code := c.Param("code")
	i, err := strconv.Atoi(code)
	if err != nil {
		i = 0
	}
	return c.String(i, "")
}

func nabeatsu(c *echo.Context) error {
	num, err := strconv.Atoi(c.Param("num"))
	if err != nil {
		num = 0
	}
	if num%3 == 0 {
		return c.String(http.StatusOK, fmt.Sprintf("%d\n", num))
	}

	return c.String(http.StatusInternalServerError, "AHO\n")
}
