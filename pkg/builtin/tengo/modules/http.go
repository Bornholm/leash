package modules

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	tengosdk "github.com/d5/tengo/v2"
)

const defaultHTTPTimeout = 30 * time.Second

var httpClient = &http.Client{
	Timeout: defaultHTTPTimeout,
}

var HTTPModule = map[string]tengosdk.Object{
	"get":     makeHTTPGetFn(),
	"post":    makeHTTPBodyFn("POST"),
	"put":     makeHTTPBodyFn("PUT"),
	"delete":  makeHTTPDeleteFn(),
	"request": makeHTTPRequestFn(),
}

func tengoMapToHeaders(obj tengosdk.Object) (http.Header, error) {
	var pairs map[string]tengosdk.Object

	switch m := obj.(type) {
	case *tengosdk.Map:
		pairs = m.Value
	case *tengosdk.ImmutableMap:
		pairs = m.Value
	default:
		return nil, fmt.Errorf("http: headers must be a map, got %s", obj.TypeName())
	}

	headers := http.Header{}
	for k, v := range pairs {
		s, ok := v.(*tengosdk.String)
		if !ok {
			return nil, fmt.Errorf("http: header value for %q must be a string, got %s", k, v.TypeName())
		}
		headers.Set(k, s.Value)
	}
	return headers, nil
}

func makeResponse(status int, body string, respHeaders http.Header, errMsg string) *tengosdk.ImmutableMap {
	headerMap := map[string]tengosdk.Object{}
	for k, vals := range respHeaders {
		if len(vals) > 0 {
			headerMap[k] = &tengosdk.String{Value: vals[0]}
		}
	}

	return &tengosdk.ImmutableMap{
		Value: map[string]tengosdk.Object{
			"status":  &tengosdk.Int{Value: int64(status)},
			"body":    &tengosdk.String{Value: body},
			"err":     &tengosdk.String{Value: errMsg},
			"headers": &tengosdk.ImmutableMap{Value: headerMap},
		},
	}
}

func doRequest(method, url string, body io.Reader, contentType string, extraHeaders http.Header) (tengosdk.Object, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return makeResponse(0, "", nil, fmt.Sprintf("http: build request: %s", err.Error())), nil
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, vals := range extraHeaders {
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return makeResponse(0, "", nil, fmt.Sprintf("http: %s %s: %s", method, url, err.Error())), nil
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return makeResponse(resp.StatusCode, "", resp.Header, fmt.Sprintf("http: read body: %s", err.Error())), nil
	}

	return makeResponse(resp.StatusCode, string(rawBody), resp.Header, ""), nil
}

func requireString(obj tengosdk.Object, ctx string) (string, error) {
	s, ok := obj.(*tengosdk.String)
	if !ok {
		return "", fmt.Errorf("%s must be a string, got %s", ctx, obj.TypeName())
	}
	return s.Value, nil
}

func makeHTTPGetFn() *tengosdk.UserFunction {
	return &tengosdk.UserFunction{
		Name: "get",
		Value: func(args ...tengosdk.Object) (tengosdk.Object, error) {
			if len(args) < 1 || len(args) > 2 {
				return nil, fmt.Errorf("http.get: expected 1 or 2 arguments, got %d", len(args))
			}

			url, err := requireString(args[0], "http.get: url")
			if err != nil {
				return nil, err
			}

			var extraHeaders http.Header
			if len(args) == 2 {
				extraHeaders, err = tengoMapToHeaders(args[1])
				if err != nil {
					return nil, err
				}
			}

			return doRequest(http.MethodGet, url, nil, "", extraHeaders)
		},
	}
}

func makeHTTPBodyFn(method string) *tengosdk.UserFunction {
	fnName := strings.ToLower(method)
	return &tengosdk.UserFunction{
		Name: fnName,
		Value: func(args ...tengosdk.Object) (tengosdk.Object, error) {
			if len(args) < 3 || len(args) > 4 {
				return nil, fmt.Errorf("http.%s: expected 3 or 4 arguments, got %d", fnName, len(args))
			}

			url, err := requireString(args[0], "http."+fnName+": url")
			if err != nil {
				return nil, err
			}

			bodyStr, err := requireString(args[1], "http."+fnName+": body")
			if err != nil {
				return nil, err
			}

			contentType, err := requireString(args[2], "http."+fnName+": content_type")
			if err != nil {
				return nil, err
			}

			var extraHeaders http.Header
			if len(args) == 4 {
				extraHeaders, err = tengoMapToHeaders(args[3])
				if err != nil {
					return nil, err
				}
			}

			return doRequest(method, url, strings.NewReader(bodyStr), contentType, extraHeaders)
		},
	}
}

func makeHTTPDeleteFn() *tengosdk.UserFunction {
	return &tengosdk.UserFunction{
		Name: "delete",
		Value: func(args ...tengosdk.Object) (tengosdk.Object, error) {
			if len(args) < 1 || len(args) > 2 {
				return nil, fmt.Errorf("http.delete: expected 1 or 2 arguments, got %d", len(args))
			}

			url, err := requireString(args[0], "http.delete: url")
			if err != nil {
				return nil, err
			}

			var extraHeaders http.Header
			if len(args) == 2 {
				extraHeaders, err = tengoMapToHeaders(args[1])
				if err != nil {
					return nil, err
				}
			}

			return doRequest(http.MethodDelete, url, nil, "", extraHeaders)
		},
	}
}

func makeHTTPRequestFn() *tengosdk.UserFunction {
	return &tengosdk.UserFunction{
		Name: "request",
		Value: func(args ...tengosdk.Object) (tengosdk.Object, error) {
			if len(args) != 5 {
				return nil, fmt.Errorf("http.request: expected 5 arguments, got %d", len(args))
			}

			method, err := requireString(args[0], "http.request: method")
			if err != nil {
				return nil, err
			}

			url, err := requireString(args[1], "http.request: url")
			if err != nil {
				return nil, err
			}

			bodyStr, err := requireString(args[2], "http.request: body")
			if err != nil {
				return nil, err
			}

			contentType, err := requireString(args[3], "http.request: content_type")
			if err != nil {
				return nil, err
			}

			extraHeaders, err := tengoMapToHeaders(args[4])
			if err != nil {
				return nil, err
			}

			var bodyReader io.Reader
			if bodyStr != "" {
				bodyReader = strings.NewReader(bodyStr)
			}

			return doRequest(strings.ToUpper(method), url, bodyReader, contentType, extraHeaders)
		},
	}
}
