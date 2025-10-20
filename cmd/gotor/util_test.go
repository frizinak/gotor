package main

import "testing"

func TestCommonAncestor(t *testing.T) {
	type tcase struct {
		in  []string
		out string
	}

	list := []tcase{
		{in: []string{"/", ""}, out: ""},
		{in: []string{"", ""}, out: ""},
		{in: []string{"", "/"}, out: ""},
		{in: []string{"/var", "/"}, out: "/"},
		{in: []string{"/var/lib/golang", ""}, out: ""},
		{in: []string{"/", "test", "/var"}, out: ""},
		{in: []string{"test", "/var", "/"}, out: ""},
		{in: []string{"/var", "test", "/"}, out: ""},
		{
			in:  []string{"/var/lib/golang/", "/var/lib/something"},
			out: "/var/lib",
		},
		{
			in:  []string{"/var/lib/golang", "/var/lib/golang/src", "/var/lib/golang/pkg"},
			out: "/var/lib/golang",
		},
		{
			in:  []string{"var/lib/golang", "var/lib/golang/src", "var/lib/golang/pkg"},
			out: "var/lib/golang",
		},
	}

	for i, c := range list {
		val := commonAncestor(c.in)
		if val != c.out {
			t.Errorf("'%s' != expected '%s' (case %d)", val, c.out, i+1)
		}
	}
}
