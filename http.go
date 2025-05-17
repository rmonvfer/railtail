package main

import (
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
)

// fwdHttp forwards an HTTP request to the target and returns any error.
func fwdHttp(outboundClient *http.Client, targetAddr string,
	w http.ResponseWriter, r *http.Request) error {

	var (
		mu          sync.Mutex
		proxyError  error
		parsedError bool
	)

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			targetURL, err := url.Parse(targetAddr + req.URL.RequestURI())
			if err != nil {
				mu.Lock()
				proxyError = errors.New("invalid target URL: " + err.Error())
				parsedError = true
				mu.Unlock()
				return
			}

			req.URL = targetURL
			req.Host = targetURL.Host

			for _, h := range hopHeaders {
				req.Header.Del(h)
			}
		},
		Transport: outboundClient.Transport,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			http.Error(w, "Error proxying request: "+err.Error(), http.StatusBadGateway)
			mu.Lock()
			proxyError = err
			mu.Unlock()
		},
	}

	mu.Lock()
	if parsedError {
		err := proxyError
		mu.Unlock()
		return err
	}
	mu.Unlock()

	proxy.ServeHTTP(w, r)

	mu.Lock()
	defer mu.Unlock()
	return proxyError
}

// hopHeaders are stripped on the way out.
var hopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailers",
	"Transfer-Encoding",
	"Upgrade",
	"Proxy-Connection",
}
