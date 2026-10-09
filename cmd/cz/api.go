package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/cli"
	"github.com/spf13/cobra"
)

func apiCmd(a *app) *cobra.Command {
	var fields []string
	var input string
	var form bool
	cmd := &cobra.Command{
		Use:   "api <METHOD> <path>",
		Short: "Make an authenticated request and print the response body",
		Long: "Fields go in the query string for GET and HEAD, otherwise in a JSON body (or a form body with --form).\n" +
			"A field value of true, false or an integer is sent as that JSON type.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			method, path := strings.ToUpper(args[0]), args[1]
			store, err := a.newStore(false)
			if err != nil {
				return err
			}
			creds, _, err := cli.Resolve(a.getenv, store)
			if err != nil {
				return err
			}
			body, header, path, err := a.buildRequest(method, path, fields, input, form)
			if err != nil {
				return err
			}
			resp, err := a.client(creds).Do(cmd.Context(), method, path, body, header)
			if err != nil {
				return err
			}
			return a.printBody(resp.Body)
		},
	}
	cmd.Flags().StringArrayVarP(&fields, "field", "f", nil, "request field `key=value` (repeatable)")
	cmd.Flags().StringVar(&input, "input", "", "send this file as the body (- for stdin)")
	cmd.Flags().BoolVar(&form, "form", false, "send fields as application/x-www-form-urlencoded")
	cmd.MarkFlagsMutuallyExclusive("field", "input")
	return cmd
}

func (a *app) buildRequest(method, path string, fields []string, input string, form bool) (io.Reader, http.Header, string, error) {
	if input != "" {
		var data []byte
		var err error
		if input == "-" {
			data, err = io.ReadAll(a.stdin)
		} else {
			data, err = os.ReadFile(input)
		}
		if err != nil {
			return nil, nil, "", err
		}
		return bytes.NewReader(data), http.Header{"Content-Type": {"application/json"}}, path, nil
	}
	if len(fields) == 0 {
		return nil, nil, path, nil
	}
	pairs := make([][2]string, 0, len(fields))
	for _, f := range fields {
		k, v, ok := strings.Cut(f, "=")
		if !ok || k == "" {
			return nil, nil, "", fmt.Errorf("field %q must be key=value", f)
		}
		pairs = append(pairs, [2]string{k, v})
	}
	if method == http.MethodGet || method == http.MethodHead {
		q := url.Values{}
		for _, p := range pairs {
			q.Add(p[0], p[1])
		}
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		return nil, nil, path + sep + q.Encode(), nil
	}
	if form {
		q := url.Values{}
		for _, p := range pairs {
			q.Add(p[0], p[1])
		}
		return strings.NewReader(q.Encode()), http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}, path, nil
	}
	obj := make(map[string]any, len(pairs))
	for _, p := range pairs {
		obj[p[0]] = typedValue(p[1])
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, nil, "", err
	}
	return bytes.NewReader(data), http.Header{"Content-Type": {"application/json"}}, path, nil
}

func typedValue(s string) any {
	switch s {
	case "true":
		return true
	case "false":
		return false
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	return s
}

func (a *app) printBody(body []byte) error {
	if a.stdoutTTY && !a.jsonFlag {
		var buf bytes.Buffer
		if json.Indent(&buf, body, "", "  ") == nil {
			body = buf.Bytes()
		}
	}
	if _, err := a.stdout.Write(body); err != nil {
		return err
	}
	if len(body) > 0 && body[len(body)-1] != '\n' {
		_, err := fmt.Fprintln(a.stdout)
		return err
	}
	return nil
}
