package nrepl_test

import (
	"net"
	"testing"
	"time"
)

// Phase 3 §32.2: arbitrary bytes, including malformed bencode and
// well-formed messages with odd ops and field types, never take the server
// down. A fresh connection still evaluates afterward.
func FuzzMessages(f *testing.F) {
	f.Add([]byte("d2:op8:describe2:id1:1e"))
	f.Add([]byte("d2:op4:eval4:code7:(+ 1 2)2:id1:2e"))
	f.Add([]byte("d2:op4:evali42e4:codeli1eee"))
	f.Add([]byte("d2:op9:interrupt12:interrupt-id1:9e"))
	f.Add([]byte("d2:op5:close7:session3:nope"))
	f.Add([]byte("i99999999999999999999999e"))
	_, server := startServer(f, 4096)
	f.Fuzz(func(t *testing.T, data []byte) {
		conn, err := net.DialTimeout("tcp", server.Addr().String(), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.SetDeadline(time.Now().Add(200 * time.Millisecond))
		_, _ = conn.Write(data)
		buf := make([]byte, 4096)
		for {
			if _, err := conn.Read(buf); err != nil {
				break // closed by the server, or idle until the deadline
			}
		}
		_ = conn.Close()
		c := dial(t, server)
		if got := responseField(c.request(t, map[string]any{"op": "eval", "code": "(+ 40 2)", "id": "check"}), "value"); got != "42" {
			t.Fatalf("server no longer evaluates after %q: %q", data, got)
		}
	})
}
