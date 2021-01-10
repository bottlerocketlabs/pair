package handlers

import (
	"net/http"
	"testing"
)

func TestGetNumberOfRecievers(t *testing.T) {
	tt := map[string]int{
		"http://localhost/?n=2":  2,
		"http://localhost/?n=4":  4,
		"http://localhost/?n=-1": 1,
		"http://localhost/":      1,
	}
	for uri, expected := range tt {
		req, err := http.NewRequest(http.MethodGet, uri, nil)
		if err != nil {
			t.Errorf("error with uri: %s", err)
		}
		n := getNumberOfRecievers(req)
		if n != expected {
			t.Errorf("\nfor url %q expected %d got %d", uri, expected, n)
		}
	}
}
