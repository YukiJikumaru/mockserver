package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v5"
)

const sequenceAttemptHeader = "X-Sequence-Attempt"

const sleepStepPrefix = "sleep:"

// sequenceAttempt は /sequence/:key が受けた要求 1 回分の記録
type sequenceAttempt struct {
	Attempt     int               `json:"attempt"`
	Time        time.Time         `json:"time"`
	SincePrevMs int64             `json:"since_prev_ms"`
	Method      string            `json:"method"`
	Step        string            `json:"step"`
	Headers     map[string]string `json:"headers"`
	Body        string            `json:"body"`
}

type sequenceHistory struct {
	Key      string            `json:"key"`
	Attempts []sequenceAttempt `json:"attempts"`
}

// stepRun は steps の 1 項目。"503*3" なら step が "503"、count が 3
type stepRun struct {
	step  string
	count int
}

// sequenceStore は key ごとに受けた要求を持つ。サーバーのメモリにだけ持つので、再起動すると消える
type sequenceStore struct {
	mu       sync.Mutex
	attempts map[string][]sequenceAttempt
}

func newSequenceStore() *sequenceStore {
	return &sequenceStore{attempts: map[string][]sequenceAttempt{}}
}

// record は 1 回分を記録し、何回目か・使う step・前の要求からの間隔を埋めて返す。
// 同じ key に同時に要求が来ても回数が重ならないよう、数えるのと記録するのを 1 つのロックの中で行う
func (s *sequenceStore) record(key string, runs []stepRun, a sequenceAttempt) sequenceAttempt {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.attempts[key]
	a.Attempt = len(list) + 1
	a.Step = stepAt(runs, len(list))
	if len(list) > 0 {
		a.SincePrevMs = a.Time.Sub(list[len(list)-1].Time).Milliseconds()
	}
	s.attempts[key] = append(list, a)
	return a
}

func (s *sequenceStore) history(key string) sequenceHistory {
	s.mu.Lock()
	defer s.mu.Unlock()
	// 受けたことのない key でも、JSON で null ではなく [] になるよう空の配列にする
	return sequenceHistory{Key: key, Attempts: append([]sequenceAttempt{}, s.attempts[key]...)}
}

func (s *sequenceStore) reset(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.attempts, key)
}

func (s *sequenceStore) handle(c *echo.Context) error {
	runs, err := parseSteps(c.QueryParam("steps"))
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error()+"\n")
	}
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return err
	}
	a := s.record(c.Param("key"), runs, sequenceAttempt{
		Time:    time.Now(),
		Method:  c.Request().Method,
		Headers: flattenHeaders(c.Request()),
		Body:    string(body),
	})
	c.Response().Header().Set(sequenceAttemptHeader, strconv.Itoa(a.Attempt))

	switch {
	case a.Step == modeEOF || a.Step == modeReset:
		return closeWithoutResponse(c, a.Step)
	case strings.HasPrefix(a.Step, sleepStepPrefix):
		ms, _ := strconv.Atoi(strings.TrimPrefix(a.Step, sleepStepPrefix))
		time.Sleep(time.Duration(ms) * time.Millisecond)
		return c.String(http.StatusOK, fmt.Sprintf("attempt %d: %s\n", a.Attempt, a.Step))
	default:
		code, _ := strconv.Atoi(a.Step)
		// 204 と 304 は本文を持てないので、本文を書くと Go の net/http がエラーにする
		if code == http.StatusNoContent || code == http.StatusNotModified {
			return c.NoContent(code)
		}
		return c.String(code, fmt.Sprintf("attempt %d: %s\n", a.Attempt, a.Step))
	}
}

func (s *sequenceStore) handleHistory(c *echo.Context) error {
	return c.JSONPretty(http.StatusOK, s.history(c.Param("key")), "  ")
}

func (s *sequenceStore) handleReset(c *echo.Context) error {
	s.reset(c.Param("key"))
	return c.String(http.StatusOK, "reset\n")
}

// parseSteps は "reset,503*2,200" を step と回数の組に分ける。
// 繰り返しを展開しないのは、"200*100000000" のような指定でメモリを使い果たさないため
func parseSteps(raw string) ([]stepRun, error) {
	if raw == "" {
		return nil, errors.New("steps を指定する（例: steps=reset,503*2,200）")
	}
	var runs []stepRun
	for _, item := range strings.Split(raw, ",") {
		run := stepRun{step: item, count: 1}
		if step, n, ok := strings.Cut(item, "*"); ok {
			count, err := strconv.Atoi(n)
			if err != nil || count < 1 {
				return nil, fmt.Errorf("回数は 1 以上の整数: %q", item)
			}
			run = stepRun{step: step, count: count}
		}
		if err := validateStep(run.step); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, nil
}

func validateStep(step string) error {
	switch {
	case step == modeEOF || step == modeReset:
		return nil
	case strings.HasPrefix(step, sleepStepPrefix):
		ms, err := strconv.Atoi(strings.TrimPrefix(step, sleepStepPrefix))
		if err != nil || ms < 0 {
			return fmt.Errorf("sleep: の後は 0 以上のミリ秒: %q", step)
		}
		return nil
	default:
		code, err := strconv.Atoi(step)
		if err != nil || code < 200 || code > 599 {
			return fmt.Errorf("step は 200〜599 のステータスコード・eof・reset・sleep:ミリ秒 のどれか: %q", step)
		}
		return nil
	}
}

// stepAt は i 回目（0 始まり）に使う step を返す。使い切ったら最後の step を返す
func stepAt(runs []stepRun, i int) string {
	for _, r := range runs {
		if i < r.count {
			return r.step
		}
		i -= r.count
	}
	return runs[len(runs)-1].step
}

// flattenHeaders は同じ名前のヘッダーを ", " でつなぐ。Go の net/http は Host をヘッダーから外すので足し戻す
func flattenHeaders(req *http.Request) map[string]string {
	h := map[string]string{"Host": req.Host}
	for k, v := range req.Header {
		h[k] = strings.Join(v, ", ")
	}
	return h
}
