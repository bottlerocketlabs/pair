package env

import "strings"

// Map turns a slice of = separated strings into a map
func Map(env []string) map[string]string {
	m := make(map[string]string)
	for _, e := range env {
		kv := strings.SplitN(e, "=", 2)
		m[kv[0]] = kv[1]
	}
	return m
}

// FirstNonBlank returns the first option that is not an empty string
func FirstNonBlank(opt ...string) string {
	for _, o := range opt {
		if o != "" {
			return o
		}
	}
	return opt[len(opt)-1]
}
