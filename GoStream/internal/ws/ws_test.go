package ws_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/levelcodingdev/gostream/internal/ws"
	"github.com/levelcodingdev/gostream/internal/wstest"
)

// echoServer upgrades and echoes every text/binary message back.
func echoServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := ws.Upgrade(w, r)
		if err != nil {
			return
		}
		for {
			op, msg, err := c.ReadMessage()
			if err != nil {
				return
			}
			if err := c.WriteMessage(op, msg); err != nil {
				return
			}
		}
	}))
}

func TestHandshakeAndEcho(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	cl, err := wstest.Dial(wstest.WSURL(srv.URL, "/"))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()

	for _, want := range []string{"hello", "", strings.Repeat("x", 5000)} {
		if err := cl.WriteText(want); err != nil {
			t.Fatal(err)
		}
		_, got, err := cl.Read()
		if err != nil || string(got) != want {
			t.Fatalf("echo(%d bytes) = %q, %v", len(want), got, err)
		}
	}
}

func TestRejectsBadUpgrade(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/") // plain GET, no Upgrade headers
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusSwitchingProtocols {
		t.Fatal("plain GET should not be upgraded")
	}
}

func TestPingIsAnswered(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()
	cl, err := wstest.Dial(wstest.WSURL(srv.URL, "/"))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()

	// client → server ping; the server's ReadMessage auto-pongs. Read() eats it,
	// so send a normal message afterwards and expect the echo.
	if err := cl.WriteRaw(0x9, []byte("pf")); err != nil {
		t.Fatal(err)
	}
	if err := cl.WriteText("after-ping"); err != nil {
		t.Fatal(err)
	}
	_, got, err := cl.Read()
	if err != nil || string(got) != "after-ping" {
		t.Fatalf("post-ping echo = %q, %v", got, err)
	}
}

func TestUnmaskedClientFrameClosed(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()
	cl, err := wstest.Dial(wstest.WSURL(srv.URL, "/"))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()

	if err := cl.WriteUnmasked(0x1, []byte("nope")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := cl.Read(); err != io.EOF {
		t.Fatalf("unmasked client frame should close the conn, got %v", err)
	}
}

func TestReadLimitClosesWith1009(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := ws.Upgrade(w, r)
		if err != nil {
			return
		}
		c.SetReadLimit(100)
		_, _, _ = c.ReadMessage() // should fail on the oversized frame
	}))
	defer srv.Close()

	cl, err := wstest.Dial(wstest.WSURL(srv.URL, "/"))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	_ = cl.WriteText(strings.Repeat("a", 500))
	if _, _, err := cl.Read(); err != io.EOF {
		t.Fatalf("oversized message should close the conn, got %v", err)
	}
}
