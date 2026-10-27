// Command example decodes a few sign-up requests the way an HTTP handler
// would, showing what kanna-validate generates: plain Validate methods, and
// errors that carry Rails' codes under the fields' JSON names.
//
// Regenerate with:
//
//	go generate ./...
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/go-kanna/validate"

	"github.com/go-kanna/validate/example/user"
)

func main() {
	requests := []struct {
		title string
		body  string
	}{
		{
			title: "A valid request",
			body: `{"name":"Kanna","email":"kanna@example.com","age":30,"role":"admin",
				"tags":["go"],"address":{"zip":"100-0001","city":"Tokyo"},
				"password":"correct horse","confirm":"correct horse"}`,
		},
		{
			// The email is rejected while decoding, by its type; everything else
			// is found by the generated Validate. Both arrive together.
			title: "Everything wrong at once",
			body: `{"name":"","email":"not an address","age":17,"role":"owner",
				"tags":["go","a tag far longer than twenty"],"website":"mailto:kanna@example.com",
				"address":{"zip":"1000001"},"password":"short","confirm":"shorter"}`,
		},
		{
			title: "Nothing sent",
			body:  `{}`,
		},
		{
			// No tag can say this; the validate method in package user does.
			title: "An admin outside the company",
			body: `{"name":"Kanna","email":"kanna@example.org","age":30,"role":"admin",
				"address":{"zip":"100-0001","city":"Tokyo"},
				"password":"correct horse","confirm":"correct horse"}`,
		},
		{
			// Malformed JSON is a request that cannot be read, not one that
			// breaks a rule: a handler answers it with 400 rather than 422.
			title: "Malformed JSON",
			body:  `{"name":`,
		},
	}

	for _, r := range requests {
		fmt.Fprintf(os.Stdout, "=== %s ===\n", r.title)

		var req user.CreateUserRequest
		err := validate.DecodeJSON(strings.NewReader(r.body), &req)

		errs, ok := errors.AsType[validate.Errors](err)
		switch {
		case err == nil:
			fmt.Fprintf(os.Stdout, "200 OK: %s <%s>\n\n", req.Name, req.Email)
		case ok:
			body, err := json.MarshalIndent(map[string]validate.Errors{"errors": errs}, "", "  ")
			if err != nil {
				panic(err)
			}
			fmt.Fprintf(os.Stdout, "422 Unprocessable Entity\n%s\n\n", body)
		default:
			fmt.Fprintf(os.Stdout, "400 Bad Request: %v\n\n", err)
		}
	}
}
