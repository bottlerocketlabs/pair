package random

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

func Bytes(len int) ([]byte, error) {
	r := make([]byte, len)
	if _, err := io.ReadFull(rand.Reader, r); err != nil {
		return r, fmt.Errorf("could not read random data into buffer: %w", err)
	}
	return r, nil
}

func String(len int) (string, error) {
	b, err := Bytes(len)
	if err != nil {
		return "", err
	}
	r := base64.URLEncoding.EncodeToString(b)
	return r, nil
}
