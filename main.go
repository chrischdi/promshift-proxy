package main

import (
	"bytes"
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	json "github.com/json-iterator/go"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
)

var (
	target = flag.String("target", "http://prometheus:9090", "target url to forward requests to")
	listen = flag.String("listen", "0.0.0.0:9090", "listen addr")
	debug  = flag.Bool("debug", false, "log level")

	targetURL *url.URL
)

const (
	pathReduceMonthOverlap = "/reduce-month-overlap"
)

func main() {
	flag.Parse()

	var err error

	targetURL, err = url.Parse(*target)
	if err != nil {
		log.Fatalf("url.Parse(%s): %v", *target, err)
	}

	log.Printf("Listening on %s", *listen)
	http.HandleFunc(pathReduceMonthOverlap+"/", removeMonthOverlap)
	// http.HandleFunc("/", forward)
	if err := http.ListenAndServe(*listen, nil); err != nil {
		log.Fatalf("Error handling: %v", err)
	}
}

func forward(w http.ResponseWriter, req *http.Request) {
	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	log.Printf("[fwr] Request: %s %s %s %s %s", req.Method, req.Host, req.URL.String(), req.RequestURI, req.URL.RawQuery)
	proxy.ServeHTTP(w, req)
}

func removeMonthOverlap(w http.ResponseWriter, req *http.Request) {
	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	log.Printf("[rmo] Request: %s %s %s %s %s", req.Method, req.Host, req.URL.String(), req.RequestURI, req.URL.RawQuery)
	if *debug {
		out, _ := httputil.DumpRequest(req, true)
		log.Printf("Request: %s", out)
	}
	// rewrite the director to trim path prefix on forwarding the request
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		requestTrimPathPrefix(req, pathReduceMonthOverlap)
		// enforce no encoding so we are able to modify the response properly
		req.Header.Del("Accept-Encoding")
	}
	// add ModifyResponse func
	proxy.ModifyResponse = func(r *http.Response) error {
		err := modifyResponse(r)
		if err != nil {
			log.Printf("error on modifyFunc: %v", err)
		}
		return err
	}
	proxy.ServeHTTP(w, req)
}

func requestTrimPathPrefix(req *http.Request, prefix string) {
	req.RequestURI = strings.TrimPrefix(req.RequestURI, prefix)
	req.URL.Path = strings.TrimPrefix(req.URL.Path, prefix)
}

func modifyResponse(r *http.Response) error {
	if *debug {
		out, _ := httputil.DumpResponse(r, true)
		log.Printf("Incoming Response: %s", out)
	}

	oldBody, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return errors.Wrap(err, "read old body")
	}

	apiResponse := APIResponse{}
	if err := json.Unmarshal(oldBody, &apiResponse); err != nil {
		r.Body = ioutil.NopCloser(bytes.NewReader(oldBody))
		r.ContentLength = int64(len(oldBody))
		r.Header.Set("Content-Length", strconv.Itoa(len(oldBody)))
		log.Printf("ignoring unmarshal APIResponse error: %v", err)
		return nil
	}

	if apiResponse.Status != "success" {
		r.Body = ioutil.NopCloser(bytes.NewReader(oldBody))
		r.ContentLength = int64(len(oldBody))
		r.Header.Set("Content-Length", strconv.Itoa(len(oldBody)))
		return nil
	}

	if apiResponse.Data == nil {
		r.Body = ioutil.NopCloser(bytes.NewReader(oldBody))
		r.ContentLength = int64(len(oldBody))
		r.Header.Set("Content-Length", strconv.Itoa(len(oldBody)))
		return nil
	}

	v := struct {
		Type   model.ValueType `json:"resultType"`
		Result json.RawMessage `json:"result"`
	}{}
	if err := json.Unmarshal(apiResponse.Data, &v); err != nil {
		return errors.Wrap(err, "unmarshal APIResponse.Data")
	}

	if v.Type != model.ValMatrix {
		r.Body = ioutil.NopCloser(bytes.NewReader(oldBody))
		r.ContentLength = int64(len(oldBody))
		r.Header.Set("Content-Length", strconv.Itoa(len(oldBody)))
		log.Printf("not modifying response, got %s, expected %s", v.Type, model.ValMatrix)
		return nil
	}
	var mv model.Matrix
	if err := json.Unmarshal(v.Result, &mv); err != nil {
		return errors.Wrap(err, "unmarshal matrix")
	}

	var newMv model.Matrix
	for _, sampleStream := range mv {
		sampleStream.Values = timeshift(sampleStream.Values)
		sampleStream.Values = trimFirst(sampleStream.Values)
		sampleStream.Values = trimLast(sampleStream.Values)
		newMv = append(newMv, sampleStream)
	}

	v.Result, err = json.Marshal(newMv)
	if err != nil {
		return err
	}

	apiResponse.Data, err = json.Marshal(&v)
	if err != nil {
		return err
	}

	newBody, err := json.Marshal(&apiResponse)
	if err != nil {
		return err
	}

	// update body and set content length information
	r.Body = ioutil.NopCloser(bytes.NewReader(newBody))
	r.ContentLength = int64(len(newBody))
	r.Header.Set("Content-Length", strconv.Itoa(len(newBody)))

	if *debug {
		out, _ := httputil.DumpResponse(r, true)
		log.Printf("Modified Response: %s", out)
	}

	return nil
}

func timeshift(pairs []model.SamplePair) []model.SamplePair {
	new := []model.SamplePair{}
	for _, p := range pairs {
		if p.Timestamp.Time().Hour() < 4 {
			hours := time.Hour * time.Duration(p.Timestamp.Time().Hour())
			minutes := time.Minute * time.Duration(p.Timestamp.Time().Minute())
			seconds := time.Second * time.Duration(p.Timestamp.Time().Second())
			delta := hours + minutes + seconds + time.Second
			p.Timestamp = p.Timestamp.Add(-delta)
		}
		if p.Timestamp.Time().Hour() > 21 {
			hours := 23*time.Hour - time.Hour*time.Duration(p.Timestamp.Time().Hour())
			minutes := 59*time.Minute - time.Minute*time.Duration(p.Timestamp.Time().Minute())
			seconds := 59*time.Second - time.Second*time.Duration(p.Timestamp.Time().Second())
			delta := hours + minutes + seconds
			p.Timestamp = p.Timestamp.Add(delta)
		}
		new = append(new, p)
	}

	return new
}

func trimFirst(pairs []model.SamplePair) []model.SamplePair {
	if len(pairs) == 0 {
		return pairs
	}

	for len(pairs) > 0 && pairs[0].Timestamp.Time().Day() > 15 {
		pairs = pairs[1:]
	}

	return pairs
}

func trimLast(pairs []model.SamplePair) []model.SamplePair {
	if len(pairs) == 0 {
		return pairs
	}

	for len(pairs) > 16 && pairs[len(pairs)-1].Timestamp.Time().Day() <= 16 {
		pairs = pairs[:len(pairs)-1]
	}

	return pairs
}

type (
	ErrorType   string
	APIResponse struct {
		Status    string          `json:"status"`
		Data      json.RawMessage `json:"data"`
		ErrorType ErrorType       `json:"errorType"`
		Error     string          `json:"error"`
		Warnings  []string        `json:"warnings,omitempty"`
	}
)
