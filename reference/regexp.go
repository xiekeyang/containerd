package reference

import (
	"regexp"
)

const (
	nsLabel = `[a-z][a-z0-9]+(?:[-]+[a-z0-9]+)*`

	containerIdLable = `[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}`
)

var (
	// match compiles the string to a regular expression.
	match = regexp.MustCompile

	// ReNamespace validates that a namespace matches valid namespaces.
	//
	// Rules for domains, defined in RFC 1035, section 2.3.1, are used for
	// namespaces.
	ReNamespace = regexp.MustCompile(anchored(nsLabel + group("[.]"+group(nsLabel)) + "*"))

	// ReContainerID is a regular expression to validate names against the collection of restricted characters.
	ReContainerId = regexp.MustCompile(anchored(containerIdLable))
)

func group(s string) string {
	return `(?:` + s + `)`
}

func anchored(s string) string {
	return `^` + s + `$`
}
