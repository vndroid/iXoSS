// Command sign prints the signature for a request, using the same
// IXOSS_KEY / IXOSS_SALT the server reads.
//
//	IXOSS_KEY=$KEY IXOSS_SALT=$SALT sign '/photo.jpg?x-oss-process=image/resize,w_800'
//
// The argument is the URL as the client will send it: the object path, and the
// x-oss-process value if there is one. What comes back goes in the
// x-ixoss-signature query parameter.
//
//	/photo.jpg?x-oss-process=image/resize,w_800&x-ixoss-signature=<output>
//
// Order does not matter — the server reads both parameters by name — but the
// process chain has to match byte for byte, including the case and the order of
// its own parameters, because that string is what was signed.
package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/vndroid/iXoSS/internal/httpserver"
	"github.com/vndroid/iXoSS/internal/security"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: %s '<path>[?x-oss-process=<chain>]'\n", os.Args[0])
		os.Exit(2)
	}

	sc := security.NewDefaultConfig()
	if _, err := security.LoadConfigFromEnv(&sc); err != nil {
		die(err)
	}
	checker, err := security.New(&sc)
	if err != nil {
		die(err)
	}

	message, err := signedMessage(os.Args[1])
	if err != nil {
		die(err)
	}
	sig, err := checker.Sign(message)
	if err != nil {
		die(err)
	}

	fmt.Println(sig)
}

func signedMessage(target string) (string, error) {
	parse := url.Parse
	if strings.HasPrefix(target, "//") {
		// An HTTP request target starting with // is still a path. Parse would
		// treat its first segment as a host and sign a different object path.
		// Fragments are not sent in HTTP requests, as with Parse below.
		target, _, _ = strings.Cut(target, "#")
		parse = url.ParseRequestURI
	}
	u, err := parse(target)
	if err != nil {
		return "", err
	}

	path := u.Path
	if path == "" || path[0] != '/' {
		path = "/" + path
	}

	// Re-read the chain through the query parser rather than trusting the raw
	// string: it is what the server will do with the URL it receives, so any
	// percent-decoding has to happen on both sides or the MACs will not match.
	return httpserver.SignedMessage(path, u.Query().Get("x-oss-process")), nil
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "sign:", err)
	os.Exit(1)
}
