package main

import (
	"bytes"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// openssl req -x509 -out localhost.crt -keyout localhost.key -newkey rsa:2048 -nodes -sha256 -subj '/CN=localhost' -extensions EXT -config <(printf "[dn]\nCN=localhost\n[req]\ndistinguished_name = dn\n[EXT]\nsubjectAltName=DNS:localhost\nkeyUsage=digitalSignature\nextendedKeyUsage=serverAuth")
var localhostCert = []byte(`-----BEGIN CERTIFICATE-----
MIIC8DCCAdigAwIBAgIUFLV01//OhRRWRCBicv+Uo4IHDcEwDQYJKoZIhvcNAQEL
BQAwFDESMBAGA1UEAwwJbG9jYWxob3N0MB4XDTI0MDIxOTA2MTUxOVoXDTI0MDMy
MDA2MTUxOVowFDESMBAGA1UEAwwJbG9jYWxob3N0MIIBIjANBgkqhkiG9w0BAQEF
AAOCAQ8AMIIBCgKCAQEAvECt1iCLWjIY9+WjPQMgeMFEb0XVYtj5YSC9PojfQFSd
9YTG9DCIfbFFCWI0vP9WyuJyBSr3gC53XIh0NMmcOltKIEbixswCrXYw7IVbLlNX
a4IC+t9SYcRF4T6mXwqtGee3p5eXrnyfSzRdv5vjtkQwMx6alypyqW9IKhJEnayk
dkbehkAlehjY53oQ79JBVholMWHfDlVV+2iw7TLeh1YZ8kWTMGnWhqJIqFAGgukY
jgOljf6k8szGMgQJE7wtDZBJbrIToHiwZlmV4Cz5R7smcFrUgoc+zKIGRIhX5AtA
4z/TEAtiRv0leiGfcaL233oTbIT4YGprJHAMW3V92QIDAQABozowODAUBgNVHREE
DTALgglsb2NhbGhvc3QwCwYDVR0PBAQDAgeAMBMGA1UdJQQMMAoGCCsGAQUFBwMB
MA0GCSqGSIb3DQEBCwUAA4IBAQCOTNwsUWOaej/QBEu4jYNY/CedJSb9gJO2m6ZW
HsBGKPOrCdh8M7KXKTno/Zn+1zilYjwV5cYnTkUYA98QsPUw8tT1QWVGo3aMwDvn
QNpO5lXhX0NneYe/IQwPALO/N+/UdkCWNBEAPJYMeXrpfDLQASagrzGI3DfIG+Yk
kiMGSAxOp3m7O2ZGUeAVShUfaSkhn4RCKzWIJXhqJVllgPGy4Os4Nq5f1/ygvaT7
0OkZcYGy2QiVzhXhVFFPzARtE+gKWwzSmRr8RYoHeWyynPojwsO89W7qUPyzCMMD
WdFZ/W7ZX2jNkPN1oLkzayMY56c7lwGNoyvnJNHPHT+N8rC3
-----END CERTIFICATE-----`)

var localhostKey = []byte(`-----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQC8QK3WIItaMhj3
5aM9AyB4wURvRdVi2PlhIL0+iN9AVJ31hMb0MIh9sUUJYjS8/1bK4nIFKveALndc
iHQ0yZw6W0ogRuLGzAKtdjDshVsuU1drggL631JhxEXhPqZfCq0Z57enl5eufJ9L
NF2/m+O2RDAzHpqXKnKpb0gqEkSdrKR2Rt6GQCV6GNjnehDv0kFWGiUxYd8OVVX7
aLDtMt6HVhnyRZMwadaGokioUAaC6RiOA6WN/qTyzMYyBAkTvC0NkElushOgeLBm
WZXgLPlHuyZwWtSChz7MogZEiFfkC0DjP9MQC2JG/SV6IZ9xovbfehNshPhgamsk
cAxbdX3ZAgMBAAECggEBAIgPW1UjCnTzNnadQ6FKTELatOKTn4mk9oELz5uOA4WU
YuJbyAwfs9bcs9/3WdSr3AghKHWxu1aqvtxjR1IL0V2gM3Amq6sxMlI61Vttexx9
Yfay7gJyPDGrUTJePJL+vWO2xVdcymVzbxJE0+kz+531pLydxu+w31S0uHyn3W3z
ZmLfWoEB22TEZsOhvPXU0FdsPL6xQZcIeGppyzueMt06/EN8+6o3nuwrOHTkBR3I
RJlmzx2ANqh/CVXVnr4kKdksBvR7r705m+K0yNHWhEowYQG/3IPUAwlCUPVv/PAp
8J3dO/2MQtpkYP+zrYmz7fbg8hQ00NOJttdpQOcJtT0CgYEA7LBx2jzF0ZU9OmLx
Wo2bu2WSpF5kaz39CNEdnZjA9fkw3fzQ0pyCSCF40ICAGKlgOdwt75CXVYMkCnoq
nrOXMO5owUjDpfE01n/0UhOMrMt7R9O3RpZ7/96qqoSX6uX2gy2cAzllTvUuAP9a
1G4d7w0ngmB7LmV1hGUI8Y9QH+MCgYEAy5yTIe9LbKNnX8JdjEzgdvev8ep73rno
+vY2nZaZtJDnEVipZG2ZvAsp6L5hyeC0WQ/pHFMMb7bL04gUBRxZfv4QDFIKUKVq
fPSL7pTe1FInoPzj5nOAjFJyYHrpbLzXuWcyv4+b9haCMIluH64lXYuJ9Aay9Vmw
6i3NWOqaYBMCgYBFq45n2fm0voQ+Wn+G0yYjHDv1DB/s/VstLDjWee9auBYtUcy6
If2rc8bXQOvz1m+PuIMBAIO7VRXXy6wd+OkpKxREyMFmaEDQXuuw+TYwatkIBIbv
TT6Ey6ta9Bqvty8/374LqOpsOnlnK8M1wpAnHK0jkHjR6GWWKhNe33uLVwKBgHzl
sddKG2Erpvi1mH68laBEdVgW9x77uW9gYsYuIGED1cntMhNZq8kOrAmJ5yBXxyku
ulbWlrVcUxGR0n4EEt30aNG17X/kyDqYI9IY3csuDgMRvZaaWUs6M9D89HzwJgAj
zpx1Mj01N20gkD28rE0CkH/xMAuSlhRAPkuBtWqjAoGBANu+QkOOOy7ded6Xhkym
uTdyYPUw34ZiITwr0ZZbvPjx6Lz6BmVqEIRJr/Smj2BvtFkjY60Jwk1GxXPF89St
7ucN09JVhYe/jZUW5Va8rDM/dJ8ZRTqYxmZT9jH/fnElBho028WkXhKexkLaDYib
0JujJzGjjomJftTU+sPV8ZwR
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
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.BodyDump(func(c echo.Context, reqBody, resBody []byte) {
		log.Println(string(reqBody))
	}))
	if *gzipEnable {
		e.Use(middleware.GzipWithConfig(middleware.GzipConfig{}))
	}

	e.Any("/", dump)
	e.GET("/help", help)
	e.GET("/nothing", nothing)
	e.GET("/streaming", streaming)
	e.GET("/streaming/infinite", streamingInfinite)
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

	s := &http.Server{
		Addr:              fmt.Sprintf(":%d", *port),
		ReadTimeout:       time.Duration(*readTimeoutMsec) * time.Millisecond,
		ReadHeaderTimeout: time.Duration(*readHeaderTimeoutMsec) * time.Millisecond,
		WriteTimeout:      time.Duration(*writeTimeoutMsec) * time.Millisecond,
		IdleTimeout:       time.Duration(*idleTimeoutMsec) * time.Millisecond,
		TLSConfig:         tlsConfig,
	}
	log.Println(s)

	e.Logger.Fatal(e.StartServer(s))
}

func dump(c echo.Context) error {
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

func hello(c echo.Context) error {
	return c.String(http.StatusOK, "Hello, World!\n")
}

func help(c echo.Context) error {
	msg := `
curl http://localhost:{{.port}}/
curl http://localhost:{{.port}}/dump
curl http://localhost:{{.port}}/nothing
curl http://localhost:{{.port}}/streaming
curl http://localhost:{{.port}}/streaming/infinite
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

func nothing(c echo.Context) error {
	return c.String(http.StatusOK, "")
}

func streaming(c echo.Context) error {
	c.Response().WriteHeader(http.StatusOK)
	cnt := 5
	for i := 1; i <= cnt; i++ {
		m := fmt.Sprintf("Streaming %d/%d\n", i, cnt)
		_, err := c.Response().Write([]byte(m))
		if err != nil {
			return err
		}
		c.Response().Flush()
		time.Sleep(1 * time.Second)
	}
	return nil
}

func streamingInfinite(c echo.Context) error {
	c.Response().WriteHeader(http.StatusOK)
	for {
		_, err := c.Response().Write([]byte("HELLO\n"))
		if err != nil {
			return err
		}
		c.Response().Flush()
		time.Sleep(1 * time.Second)
	}
}

func sleep(c echo.Context) error {
	msec := c.QueryParam("msec")
	i, err := strconv.Atoi(msec)
	if err != nil {
		i = 0
	}

	time.Sleep(time.Millisecond * time.Duration(i))

	return c.String(http.StatusOK, fmt.Sprintf("Sleep %d milliseconds.", i))
}

func validjson(c echo.Context) error {
	c.Response().Header().Set(echo.HeaderContentType, echo.MIMEApplicationJSONCharsetUTF8)
	return c.String(http.StatusOK, "{\"name\":\"hello\"}")
}

func invalidjson(c echo.Context) error {
	c.Response().Header().Set(echo.HeaderContentType, echo.MIMEApplicationJSONCharsetUTF8)
	return c.String(http.StatusOK, "{\"name\":\"hello")
}

func invalidlengthTooShort(c echo.Context) error {
	c.Response().Header().Set(echo.HeaderContentLength, "100")
	return c.String(http.StatusOK, "Hello")
}

func invalidlengthTooLong(c echo.Context) error {
	c.Response().Header().Set(echo.HeaderContentLength, "1")
	return c.String(http.StatusOK, "Hello")
}

func statuscode(c echo.Context) error {
	code := c.Param("code")
	i, err := strconv.Atoi(code)
	if err != nil {
		i = 0
	}
	return c.String(i, "")
}

func nabeatsu(c echo.Context) error {
	num, err := strconv.Atoi(c.Param("num"))
	if err != nil {
		num = 0
	}
	if num%3 == 0 {
		return c.String(http.StatusOK, fmt.Sprintf("%d\n", num))
	}

	return c.String(http.StatusInternalServerError, "AHO\n")
}
