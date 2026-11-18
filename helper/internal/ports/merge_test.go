package ports

import (
	"reflect"
	"testing"
)

// A reloader and its worker both hold port 8000, on both loopback addresses.
func TestNormalizeListsAPortOnce(t *testing.T) {
	got := normalize([]Listener{
		{Port: 8000, Address: "::1", PID: 510, Command: "python3"},
		{Port: 8000, Address: "127.0.0.1", PID: 510, Command: "python3"},
		{Port: 8000, Address: "127.0.0.1", PID: 498, Command: "uvicorn"},
		{Port: 5432, Address: "0.0.0.0", PID: 90, Command: "postgres"},
		{Port: 5432, Address: "::", PID: 90, Command: "postgres"},
		{Port: 3000, Address: "::1", PID: 7, Command: "node"},
	})
	want := []Listener{
		{Port: 3000, Address: "::1", PID: 7, Command: "node"},
		{Port: 5432, Address: "0.0.0.0", PID: 90, Command: "postgres"},
		{Port: 8000, Address: "127.0.0.1", PID: 498, Command: "uvicorn"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %+v\nwant %+v", got, want)
	}
}
