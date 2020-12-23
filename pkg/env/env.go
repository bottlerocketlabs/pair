package env

import "strings"

func Map(env []string) map[string]string {
	m := make(map[string]string)
	for _, e := range env {
		kv := strings.SplitN(e, "=", 2)
		m[kv[0]] = kv[1]
	}
	return m
}
