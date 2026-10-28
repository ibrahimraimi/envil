package state

import (
	"testing"
)

func FuzzParseStateBytes(f *testing.F) {
	sampleKey := []byte("0123456789abcdef0123456789abcdef")

	f.Add([]byte(`{}`))
	f.Add([]byte(`{"schema":1,"projects":{},"mac":"invalid"}`))
	f.Add([]byte(`{"schema":99,"projects":{},"mac":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}`))
	f.Add([]byte(`not json at all`))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		projects, err := ParseStateBytes(data, sampleKey)
		if err == nil && projects == nil {
			t.Fatal("ParseStateBytes returned nil error with nil projects map")
		}
	})
}

