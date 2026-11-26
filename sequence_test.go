package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// 接続を使い回すと Go の Transport が自動で送り直し、回数がずれるので、毎回新しい接続で送る
var noKeepAliveClient = &http.Client{
	Transport: &http.Transport{DisableKeepAlives: true},
	Timeout:   5 * time.Second,
}

func seqURL(ts *httptest.Server, key, steps string) string {
	return ts.URL + "/sequence/" + key + "?steps=" + url.QueryEscape(steps)
}

// seqDo は要求を送り、ステータスコードと X-Sequence-Attempt を返す
func seqDo(t *testing.T, method, u string, body string, header http.Header) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, u, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	res, err := noKeepAliveClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	return res.StatusCode, res.Header.Get("X-Sequence-Attempt")
}

func assertSeq(t *testing.T, gotStatus int, gotAttempt string, wantStatus, wantAttempt int) {
	t.Helper()
	if gotStatus != wantStatus || gotAttempt != strconv.Itoa(wantAttempt) {
		t.Fatalf("want status=%d attempt=%d, got status=%d attempt=%q", wantStatus, wantAttempt, gotStatus, gotAttempt)
	}
}

// 履歴の JSON の形（テストはこの形を約束として確かめる）
type historyJSON struct {
	Key      string `json:"key"`
	Attempts []struct {
		Attempt     int               `json:"attempt"`
		Time        string            `json:"time"`
		SincePrevMs int64             `json:"since_prev_ms"`
		Method      string            `json:"method"`
		Step        string            `json:"step"`
		Headers     map[string]string `json:"headers"`
		Body        string            `json:"body"`
	} `json:"attempts"`
}

func getHistory(t *testing.T, ts *httptest.Server, key string) historyJSON {
	t.Helper()
	res, err := noKeepAliveClient.Get(ts.URL + "/sequence/" + key + "/history")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("history status=%d", res.StatusCode)
	}
	var h historyJSON
	if err := json.NewDecoder(res.Body).Decode(&h); err != nil {
		t.Fatal(err)
	}
	return h
}

func TestSequence(t *testing.T) {
	ts := startEcho(t, false)
	addr := ts.Listener.Addr().String()

	t.Run("steps を先頭から 1 つずつ使い、使い切ったら最後を繰り返す", func(t *testing.T) {
		c := dialRaw(t, addr)
		writeRequest(t, c, "GET", "/sequence/order?steps=reset,503,200", nil)
		got, err := readUntilClosed(c)
		assertReset(t, got, err)

		u := seqURL(ts, "order", "reset,503,200")
		s, a := seqDo(t, "GET", u, "", nil)
		assertSeq(t, s, a, 503, 2)
		s, a = seqDo(t, "GET", u, "", nil)
		assertSeq(t, s, a, 200, 3)
		s, a = seqDo(t, "GET", u, "", nil)
		assertSeq(t, s, a, 200, 4)
	})

	t.Run("X*N で同じ step を N 回使う", func(t *testing.T) {
		u := seqURL(ts, "repeat", "503*2,200")
		for i, want := range []int{503, 503, 200} {
			s, a := seqDo(t, "GET", u, "", nil)
			assertSeq(t, s, a, want, i+1)
		}
	})

	t.Run("eof は応答せずに閉じる", func(t *testing.T) {
		c := dialRaw(t, addr)
		writeRequest(t, c, "POST", "/sequence/eof?steps=eof,200", []byte("body"))
		got, err := readUntilClosed(c)
		assertEOF(t, got, err)

		s, a := seqDo(t, "POST", seqURL(ts, "eof", "eof,200"), "body", nil)
		assertSeq(t, s, a, 200, 2)
	})

	t.Run("sleep:ミリ秒 は待ってから 200 を返す", func(t *testing.T) {
		u := seqURL(ts, "sleep", "sleep:300,200")
		start := time.Now()
		s, a := seqDo(t, "GET", u, "", nil)
		if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
			t.Fatalf("300 ミリ秒は待つはず: %v", elapsed)
		}
		assertSeq(t, s, a, 200, 1)
		s, a = seqDo(t, "GET", u, "", nil)
		assertSeq(t, s, a, 200, 2)
	})

	t.Run("本文を持てないステータスコードも返せる", func(t *testing.T) {
		s, a := seqDo(t, "GET", seqURL(ts, "nobody", "204"), "", nil)
		assertSeq(t, s, a, 204, 1)
	})

	t.Run("key ごとに別々に数える", func(t *testing.T) {
		s, a := seqDo(t, "GET", seqURL(ts, "key-a", "503,200"), "", nil)
		assertSeq(t, s, a, 503, 1)
		s, a = seqDo(t, "GET", seqURL(ts, "key-b", "503,200"), "", nil)
		assertSeq(t, s, a, 503, 1)
		s, a = seqDo(t, "GET", seqURL(ts, "key-a", "503,200"), "", nil)
		assertSeq(t, s, a, 200, 2)
	})

	t.Run("history は受けた要求を順に返し、history 自体は数えない", func(t *testing.T) {
		u := seqURL(ts, "hist", "503,200")
		seqDo(t, "POST", u, "hello", http.Header{"Idempotency-Key": {"abc"}})
		seqDo(t, "GET", u, "", nil)

		getHistory(t, ts, "hist")
		h := getHistory(t, ts, "hist")
		if h.Key != "hist" || len(h.Attempts) != 2 {
			t.Fatalf("2 件のはず: %+v", h)
		}
		first, second := h.Attempts[0], h.Attempts[1]
		if first.Attempt != 1 || first.Method != "POST" || first.Step != "503" || first.Body != "hello" || first.Headers["Idempotency-Key"] != "abc" || first.SincePrevMs != 0 {
			t.Fatalf("1 件目: %+v", first)
		}
		if second.Attempt != 2 || second.Method != "GET" || second.Step != "200" || second.SincePrevMs < 0 {
			t.Fatalf("2 件目: %+v", second)
		}
		for _, a := range h.Attempts {
			if _, err := time.Parse(time.RFC3339Nano, a.Time); err != nil {
				t.Fatalf("time は RFC 3339 のはず: %q", a.Time)
			}
		}
	})

	t.Run("受けたことのない key の history は空", func(t *testing.T) {
		h := getHistory(t, ts, "never")
		if h.Key != "never" || h.Attempts == nil || len(h.Attempts) != 0 {
			t.Fatalf("空の配列のはず: %+v", h)
		}
	})

	t.Run("reset で数え直す", func(t *testing.T) {
		u := seqURL(ts, "reset-me", "503,200")
		s, a := seqDo(t, "GET", u, "", nil)
		assertSeq(t, s, a, 503, 1)

		s, _ = seqDo(t, "POST", ts.URL+"/sequence/reset-me/reset", "", nil)
		if s != http.StatusOK {
			t.Fatalf("reset status=%d", s)
		}
		if h := getHistory(t, ts, "reset-me"); len(h.Attempts) != 0 {
			t.Fatalf("reset で履歴も消えるはず: %+v", h)
		}

		s, a = seqDo(t, "GET", u, "", nil)
		assertSeq(t, s, a, 503, 1)
	})

	t.Run("steps が正しくなければ 400 で、回数に数えない", func(t *testing.T) {
		for _, steps := range []string{"", "abc", "99", "600", "200,,503", "503*0", "503*x", "sleep:x", "sleep:-1"} {
			s, _ := seqDo(t, "GET", seqURL(ts, "bad", steps), "", nil)
			if s != http.StatusBadRequest {
				t.Fatalf("steps=%q は 400 のはず: status=%d", steps, s)
			}
		}
		if h := getHistory(t, ts, "bad"); len(h.Attempts) != 0 {
			t.Fatalf("数えないはず: %+v", h)
		}
	})

	t.Run("同じ key に同時に送っても回数が重ならない", func(t *testing.T) {
		const n = 20
		u := seqURL(ts, "concurrent", "200")
		var mu sync.Mutex
		var attempts []int
		var wg sync.WaitGroup
		for range n {
			wg.Go(func() {
				_, a := seqDo(t, "GET", u, "", nil)
				i, _ := strconv.Atoi(a)
				mu.Lock()
				attempts = append(attempts, i)
				mu.Unlock()
			})
		}
		wg.Wait()
		sort.Ints(attempts)
		for i, a := range attempts {
			if a != i+1 {
				t.Fatalf("1〜%d が 1 回ずつのはず: %v", n, attempts)
			}
		}
	})
}

func TestSequenceHelpMentionsSequence(t *testing.T) {
	ts := startEcho(t, false)
	res, err := http.Get(ts.URL + "/help")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(body), "/sequence/") {
		t.Fatal(fmt.Sprintf("/help に /sequence の説明があるはず: %s", body))
	}
}
