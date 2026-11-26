package cli

import (
	"encoding/json"
	"testing"
)

func TestLargeIntegerArgumentsPreserved(t *testing.T) {
	got, err := readArgs(`{"id":9007199254740993}`, nil, 1000)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(got)
	if err != nil || string(b) != `{"id":9007199254740993}` {
		t.Fatalf("integer changed: %s %v", b, err)
	}
}
