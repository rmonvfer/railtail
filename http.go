// Package main provides railtail HTTP and TCP proxying functionality.
package main

import (
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
)

// fwdHttp forwards an HTTP request to the target address using the provided outbound client.
// It returns any errors encountered during forwarding.
// This function guarantees thread-safety when handling proxy errors.
func fwdHttp(outboundClient *http.Client, targetAddr string, w http.ResponseWriter, r *http.Request) error {
	// Use sync.Mutex to safely handle the error return value when accessed from multiple goroutines
	var (
		mu          sync.Mutex
		proxyError  error
		parsedError bool
	)

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			// Parse the target URL with proper error handling
			targetURL, err := url.Parse(targetAddr + req.URL.RequestURI())
			if err != nil {
				mu.Lock()
				proxyError = errors.New("invalid target URL: " + err.Error())
				parsedError = true
				mu.Unlock()
				return
			}
			
			// Update the request with the new target URL
			req.URL = targetURL
			req.Host = req.URL.Host
			
			// We don't need to manually copy headers here as ReverseProxy 
			// handles that for us. The outbound request inherits headers
			// from the original request.
			
			// Remove any hop-by-hop headers to avoid potential issues
			// This is important for proxies to prevent header leakage
			for _, h := range hopHeaders {
				req.Header.Del(h)
			}
		},
		Transport: outboundClient.Transport,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// Return a proper error response to the client
			http.Error(w, "Error proxying request: "+err.Error(), http.StatusBadGateway)

			// Safely store the error for the parent function to return
			mu.Lock()
			proxyError = err
			mu.Unlock()
		},
		ModifyResponse: func(resp *http.Response) error {
			// This is called after receiving response from the target
			// We could modify response headers here if needed
			return nil // Return nil to indicate no errors
		},
	}

	// If we had parsing errors, return immediately without serving
	mu.Lock()
	if parsedError {
		err := proxyError
		mu.Unlock()
		return err
	}
	mu.Unlock()

	// Handle the actual request
	proxy.ServeHTTP(w, r)

	// Return any errors that occurred
	mu.Lock()
	defer mu.Unlock()
	return proxyError
}

// Hop-by-hop headers that should be removed when proxying
// See: https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers#hop-by-hop_headers
var hopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailers",
	"Transfer-Encoding",
	"Upgrade",
}