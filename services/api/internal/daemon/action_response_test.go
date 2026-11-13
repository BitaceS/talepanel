package daemon

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func respWith(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}

func TestDecodeActionResponse(t *testing.T) {
	cases := []struct {
		status  int
		body    string
		ok      bool
		message string
	}{
		{200, `{"success":true,"message":"started"}`, true, "started"},
		{400, `{"error":"path escapes server directory"}`, false, "path escapes server directory"},
		{422, `Failed to deserialize the JSON body`, false, "HTTP 422: Failed to deserialize the JSON body"},
	}
	for _, c := range cases {
		got, err := decodeActionResponse(respWith(c.status, c.body))
		if err != nil {
			t.Fatalf("%q: %v", c.body, err)
		}
		if got.Success != c.ok || got.Message != c.message {
			t.Errorf("%q: got %+v", c.body, got)
		}
	}
	if _, err := decodeActionResponse(respWith(200, "not json")); err == nil {
		t.Error("garbage 200 body accepted")
	}
}
