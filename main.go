package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"text/template"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

var (
	port                  *int
	readTimeoutMsec       *int
	writeTimeoutMsec      *int
	readHeaderTimeoutMsec *int
	idleTimeoutMsec       *int
	gzipEnable            *bool
)

func main() {
	port = flag.Int("p", 3000, "binding port")
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

	e.GET("/", hello)
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

	e.POST("/", post)

	s := &http.Server{
		Addr:              fmt.Sprintf(":%d", *port),
		ReadTimeout:       time.Duration(*readTimeoutMsec) * time.Millisecond,
		ReadHeaderTimeout: time.Duration(*readHeaderTimeoutMsec) * time.Millisecond,
		WriteTimeout:      time.Duration(*writeTimeoutMsec) * time.Millisecond,
		IdleTimeout:       time.Duration(*idleTimeoutMsec) * time.Millisecond,
	}
	log.Println(s)

	e.Logger.Fatal(e.StartServer(s))
}

func hello(c echo.Context) error {
	return c.String(http.StatusOK, "Hello, World!\n")
}

func help(c echo.Context) error {
	msg := `
curl http://localhost:{{.port}}/
curl http://localhost:{{.port}}/nothing
curl http://localhost:{{.port}}/streaming
curl http://localhost:{{.port}}/streaming/infinite
curl http://localhost:{{.port}}/sleep?msec=1000
curl http://localhost:{{.port}}/json/valid
curl http://localhost:{{.port}}/json/invalid
curl http://localhost:{{.port}}/invalidlength/long
curl http://localhost:{{.port}}/invalidlength/short
curl http://localhost:{{.port}}/statuscode/:code

curl -X POST http://localhost:{{.port}}/
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

func post(c echo.Context) error {
	return c.String(http.StatusOK, "Hello, World!\n")
}
