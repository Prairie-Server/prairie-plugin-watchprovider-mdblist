package provider

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	pluginv1 "github.com/prairie-server/prairie-plugin-sdk/pkg/pluginproto/prairie/plugin/v1"
)

func TestParseRetryAfterForms(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		value string
		want  time.Duration
		ok    bool
	}{
		{"", 0, false},
		{"-5", 0, false},
		{"7", 7 * time.Second, true},
		{"99999999999999999", time.Duration(math.MaxInt64), true},
		{now.Add(30 * time.Second).Format(http.TimeFormat), 30 * time.Second, true},
		{now.Add(-time.Hour).Format(http.TimeFormat), 0, true},
		{"soon", 0, false},
	}
	for _, tc := range cases {
		got, ok := parseRetryAfter(tc.value, now)
		if got != tc.want || ok != tc.ok {
			t.Errorf("parseRetryAfter(%q) = %v, %v; want %v, %v", tc.value, got, ok, tc.want, tc.ok)
		}
	}
}

func TestSleepContext(t *testing.T) {
	t.Parallel()
	if err := sleepContext(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("short sleep: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepContext(ctx, time.Hour); err == nil {
		t.Fatal("cancelled sleep returned nil")
	}
}

func TestErrorTypesAndFaultMapping(t *testing.T) {
	t.Parallel()
	limited := &rateLimitedError{retryAfter: 3 * time.Second}
	if !strings.Contains(limited.Error(), "3s") {
		t.Fatalf("rate limit error = %q", limited.Error())
	}
	cause := errors.New("boom")
	decode := &decodeError{cause: cause}
	if !strings.Contains(decode.Error(), "boom") || !errors.Is(decode, cause) {
		t.Fatalf("decode error = %q", decode.Error())
	}
	if faultFor(nil) != nil {
		t.Fatal("nil error must map to no fault")
	}
	if got := faultFor(errMissingAPIKey).GetCode(); got != pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_INVALID_CREDENTIAL {
		t.Fatalf("missing key fault = %v", got)
	}
	if got := faultFor(decode).GetCode(); got != pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_TEMPORARY {
		t.Fatalf("decode fault = %v", got)
	}
}

func TestIDsFromProviderItemKey(t *testing.T) {
	t.Parallel()
	cases := map[string]mdblistIDs{
		" imdb:tt1 ": {IMDb: "tt1"},
		"tmdb:22":    {TMDB: 22},
		"tvdb:33":    {TVDB: 33},
		"mdblist:ab": {MDBList: "ab"},
		"other:5":    {},
		"imdb:":      {},
		"no-prefix":  {},
	}
	for key, want := range cases {
		if got := idsFromProviderItemKey(key); got != want {
			t.Errorf("idsFromProviderItemKey(%q) = %+v, want %+v", key, got, want)
		}
	}
}

func TestProgressAcceptsNumbersAndNumericStrings(t *testing.T) {
	t.Parallel()
	cases := map[string]float64{
		`null`:  0,
		`12.5`:  12.5,
		`"40%"`: 40,
		`" 7 "`: 7,
		`""`:    0,
		`"%"`:   0,
	}
	for raw, want := range cases {
		var got mdblistProgress
		if err := json.Unmarshal([]byte(raw), &got); err != nil || float64(got) != want {
			t.Errorf("progress %s = %v, %v; want %v", raw, got, err, want)
		}
	}
	for _, raw := range []string{`true`, `"abc"`} {
		var got mdblistProgress
		if err := json.Unmarshal([]byte(raw), &got); err == nil {
			t.Errorf("progress %s decoded to %v, want an error", raw, got)
		}
	}
}

func TestEmptyJSONValue(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		``:               true,
		`null`:           true,
		`false`:          true,
		`true`:           false,
		`0`:              true,
		`3`:              false,
		`" "`:            true,
		`[]`:             true,
		`[0, "", null]`:  true,
		`[1]`:            false,
		`{"a": [false]}`: true,
		`{"a": "x"}`:     false,
		`{not json`:      false,
	}
	for raw, want := range cases {
		if got := emptyJSONValue(json.RawMessage(raw)); got != want {
			t.Errorf("emptyJSONValue(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestRequestedStateKindAndCloneMap(t *testing.T) {
	t.Parallel()
	watched := pluginv1.WatchSyncRemoteStateKind_WATCH_SYNC_REMOTE_STATE_KIND_WATCHED
	if kind, fault := requestedStateKind(nil); kind != watched || fault != nil {
		t.Fatalf("default kind = %v, %v", kind, fault)
	}
	if _, fault := requestedStateKind([]pluginv1.WatchSyncRemoteStateKind{watched, watched}); fault == nil {
		t.Fatal("two kinds must be refused")
	}
	if cloneMap(nil) != nil {
		t.Fatal("empty map must clone to nil")
	}
	original := map[string]string{"k": "v"}
	clone := cloneMap(original)
	clone["k"] = "changed"
	if original["k"] != "v" {
		t.Fatal("clone shares state with the original")
	}
}
