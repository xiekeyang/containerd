package reference

import (
	"regexp"
	"testing"
)

type regexpMatch struct {
	input string
	match bool
}

func TestReNamespace(t *testing.T) {
	for _, testcase := range []regexpMatch{
		{
			input: "abcd",
			match: true,
		},
		{
			input: "default-default",
			match: true,
		},
		{
			input: "default--default",
			match: true,
		},
		{
			input: "containerd.io",
			match: true,
		},
		{
			input: "foo.boo",
			match: true,
		},
		{
			input: "swarmkit.docker.io",
			match: true,
		},
		{
			input: "zn--e9.org", // or something like it!
			match: true,
		},
		{
			input: ".foo..foo",
			match: false,
		},
		{
			input: "foo/foo",
			match: false,
		},
		{
			input: "foo/..",
			match: false,
		},
		{
			input: "foo..foo",
			match: false,
		},
		{
			input: "foo.-boo",
			match: false,
		},
		{
			input: "-foo.boo",
			match: false,
		},
		{
			input: "foo.boo-",
			match: false,
		},
		{
			input: "foo_foo.boo_underscores", // boo-urns?
			match: false,
		},
	} {
		t.Run(testcase.input, func(t *testing.T) {
			checkRegexp(t, ReNamespace, testcase)
		})
	}
}

func TestReContainerId(t *testing.T) {
	for _, testcase := range []regexpMatch{
		{
			input: "default",
			match: true,
		},
		{
			input: "default-default",
			match: true,
		},
		{
			input: "default--default",
			match: true,
		},
		{
			input: "containerd.io",
			match: true,
		},
		{
			input: "foo.boo",
			match: true,
		},
		{
			input: "swarmkit.docker.io",
			match: true,
		},
		{
			input: "zn--e9.org", // or something like it!
			match: true,
		},
		{
			input: ".foo..foo",
			match: false,
		},
		{
			input: "foo/foo",
			match: false,
		},
		{
			input: "foo/..",
			match: false,
		},
		{
			input: "foo..foo",
			match: true,
		},
		{
			input: "foo.-boo",
			match: true,
		},
		{
			input: "-foo.boo",
			match: false,
		},
		{
			input: "foo.boo-",
			match: true,
		},
		{
			input: "foo_foo.boo_underscores", // boo-urns?
			match: true,
		},
	} {
		t.Run(testcase.input, func(t *testing.T) {
			checkRegexp(t, ReContainerId, testcase)
		})
	}
}

func checkRegexp(t *testing.T, r *regexp.Regexp, m regexpMatch) {
	matches := r.FindStringSubmatch(m.input)
	if m.match && matches != nil {
		if matches[0] != m.input {
			t.Errorf("Expected matche vs get %q", m.input, matches[0])
		}
	} else if m.match {
		t.Errorf("Expected match for %q", m.input)
	} else if matches != nil {
		t.Errorf("Unexpected match for %q", m.input)
	}
}
