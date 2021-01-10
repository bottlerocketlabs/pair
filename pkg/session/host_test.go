package session

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestUnmarshalSizeMessage(t *testing.T) {
	sizeMessage := []byte(`["set_size",30,30,30,30]`)
	expected := []uint16{0, 30, 30, 30, 30}
	recieved, err := parseSizeMessage(sizeMessage)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}
	if !cmp.Equal(recieved, expected) {
		t.Errorf("got %v, expected %v", recieved, expected)
	}
}
