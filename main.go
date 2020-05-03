package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	target = flag.String("target", "http://prometheus:9090", "target url to forward requests to")
	listen = flag.String("listen", "0.0.0.0:9090", "listen addr")
	debug  = flag.Bool("debug", false, "log level")

	targetURL           *url.URL
	timeshiftHeadersMap = map[string]bool{
		"promshift_start": true,
		"promshift_end":   true,
		"promshift_time":  true,
	}
)

func main() {
	flag.Parse()

	var err error

	targetURL, err = url.Parse(*target)
	if err != nil {
		log.Fatalf("url.Parse(%s): %v", *target, err)
	}

	log.Printf("Listening on %s", *listen)
	http.HandleFunc("/", serveRequest)
	if err := http.ListenAndServe(*listen, nil); err != nil {
		log.Fatalf("Error handling: %v", err)
	}
}

func serveRequest(w http.ResponseWriter, req *http.Request) {
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	req.URL.Host = targetURL.Host
	req.URL.Scheme = targetURL.Scheme
	req.Header.Set("X-Forwarded-Host", req.Header.Get("Host"))
	req.Host = targetURL.Host

	if *debug {
		log.Printf("RawQuery before change: %s", req.URL.RawQuery)
	}
	req.URL.RawQuery = shiftRawQuery(req.URL.RawQuery)
	if *debug {
		log.Printf("RawQuery after change : %s", req.URL.RawQuery)
	}
	proxy.ServeHTTP(w, req)
}

// shiftRawQuery shifts the RawQuery based on url parameters.
// url.Values is not used because it is not compatible to the query parameter.
func shiftRawQuery(query string) string {
	changes := map[string]int64{}
	resultValues := []string{}

	// split headers to slice, filter for `promshift_` headers and parse them
	kvs := []string{}
	for _, kv := range strings.Split(query, "&") {
		if strings.HasPrefix(kv, "promshift_") {
			targetHeader, change := parsePromshiftHeader(kv)
			if targetHeader == "" {
				// error parsing so let's ignore this one
				continue
			}
			changes[targetHeader] = change
			// we do not want to keep the header
			continue
		}

		// add header to headers to keep it
		kvs = append(kvs, kv)
	}

	// apply changes
	for _, kv := range kvs {
		s := strings.Split(kv, "=")
		if len(s) != 2 {
			resultValues = append(resultValues, kv)
			continue
		}

		k := s[0]
		v := s[1]

		if c, ok := changes[k]; ok {
			i, err := strconv.ParseInt(strings.Split(v, ".")[0], 10, 64)
			if err != nil {
				log.Printf("ERROR: error parsing header %q, value %q: %v", k, v, err)
				continue
			}
			i = i + c
			v = fmt.Sprintf("%d", i)
		}

		resultValues = append(resultValues, k+"="+v)
	}

	return strings.Join(resultValues, "&")
}

// parsePromshiftHeader parses a promshift header which should be a duration
// and returns the header to be modified and the value to add.
func parsePromshiftHeader(h string) (string, int64) {
	splitted := strings.Split(h, "=")
	if !timeshiftHeadersMap[splitted[0]] {
		return "", 0
	}

	promshiftHeader := splitted[0]
	targetHeader := strings.TrimPrefix(promshiftHeader, "promshift_")
	v := splitted[1]

	dur, err := time.ParseDuration(v)
	if err != nil {
		log.Printf("WARN: unable to parse duration %q from header %q", v, promshiftHeader)
		return "", 0
	}

	change := int64(dur.Seconds())
	return targetHeader, change
}
