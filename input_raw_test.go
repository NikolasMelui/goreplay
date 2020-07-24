package main

import (
	"bytes"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buger/goreplay/capture"
	"github.com/buger/goreplay/proto"
)

const testRawExpire = time.Millisecond * 200

func TestRAWInputIPv4(t *testing.T) {
	wg := new(sync.WaitGroup)
	quit := make(chan int)

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Error(err)
		return
	}
	origin := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("ab"))
		}),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	go origin.Serve(listener)
	defer listener.Close()

	_, port, _ := net.SplitHostPort(listener.Addr().String())

	var respCounter, reqCounter int64
	conf := RAWInputConfig{
		engine:        capture.EnginePcap,
		expire:        0,
		protocol:      ProtocolHTTP,
		trackResponse: true,
		realIPHeader:  "X-Real-IP",
	}
	input := NewRAWInput("127.0.0.1:"+port, conf)

	output := NewTestOutput(func(data []byte) {
		if data[0] == '1' {
			body := payloadBody(data)
			if len(proto.Header(body, []byte("X-Real-IP"))) == 0 {
				t.Error("Should have X-Real-IP header", string(body))
			}
			atomic.AddInt64(&reqCounter, 1)
		} else {
			atomic.AddInt64(&respCounter, 1)
		}
		wg.Done()
	})

	plugins := &InOutPlugins{
		Inputs:  []io.Reader{input},
		Outputs: []io.Writer{output},
	}
	plugins.All = append(plugins.All, input, output)

	client := NewHTTPClient("http://127.0.0.1:"+port, &HTTPClientConfig{})

	emitter := NewEmitter(quit)
	go emitter.Start(plugins, Settings.middleware)
	for i := 0; i < 10; i++ {
		// request + response
		wg.Add(2)
		_, err = client.Get("http://127.0.0.1:" + port)
		if err != nil {
			t.Error(err)
			return
		}
	}
	wg.Wait()
	const want = 10
	if reqCounter != respCounter && reqCounter != want {
		t.Errorf("want %d requests and %d responses, got %d requests and %d responses", want, want, reqCounter, respCounter)
	}
	emitter.Close()
}

func TestRAWInputNoKeepAlive(t *testing.T) {
	wg := new(sync.WaitGroup)
	quit := make(chan int)

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	origin := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("ab"))
		}),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	origin.SetKeepAlivesEnabled(false)
	go origin.Serve(listener)
	defer listener.Close()

	_, port, _ := net.SplitHostPort(listener.Addr().String())
	originAddr := "127.0.0.1:" + port
	conf := RAWInputConfig{
		engine:        capture.EnginePcap,
		expire:        testRawExpire,
		protocol:      ProtocolHTTP,
		trackResponse: true,
	}
	input := NewRAWInput(originAddr, conf)
	var respCounter, reqCounter int64
	output := NewTestOutput(func(data []byte) {
		if data[0] == '1' {
			atomic.AddInt64(&reqCounter, 1)
		} else {
			atomic.AddInt64(&respCounter, 1)
		}
		wg.Done()
	})

	plugins := &InOutPlugins{
		Inputs:  []io.Reader{input},
		Outputs: []io.Writer{output},
	}
	plugins.All = append(plugins.All, input, output)

	client := NewHTTPClient("http://"+originAddr, &HTTPClientConfig{})

	emitter := NewEmitter(quit)
	go emitter.Start(plugins, Settings.middleware)

	for i := 0; i < 10; i++ {
		// request + response
		wg.Add(2)
		client.Get("/")
	}

	wg.Wait()
	const want = 10
	if reqCounter != respCounter && reqCounter != want {
		t.Errorf("want %d requests and %d responses, got %d requests and %d responses", want, want, reqCounter, respCounter)
	}
	emitter.Close()
}

func TestRAWInputIPv6(t *testing.T) {
	wg := new(sync.WaitGroup)
	quit := make(chan int)

	listener, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("ab"))
		}),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	// if you set this to true, ON IPv6 clients keep re-using the same connection, which
	// means any x number of requests may be regarded as one message!
	origin.SetKeepAlivesEnabled(false)
	go origin.Serve(listener)
	defer listener.Close()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	originAddr := "[::1]:" + port

	var respCounter, reqCounter int64
	conf := RAWInputConfig{
		engine:        capture.EnginePcap,
		protocol:      ProtocolHTTP,
		trackResponse: true,
	}
	input := NewRAWInput(originAddr, conf)

	output := NewTestOutput(func(data []byte) {
		if data[0] == '1' {
			atomic.AddInt64(&reqCounter, 1)
		} else {
			atomic.AddInt64(&respCounter, 1)
		}
		wg.Done()
	})

	plugins := &InOutPlugins{
		Inputs:  []io.Reader{input},
		Outputs: []io.Writer{output},
	}

	client := NewHTTPClient("http://"+originAddr, &HTTPClientConfig{})

	emitter := NewEmitter(quit)
	go emitter.Start(plugins, Settings.middleware)
	for i := 0; i < 10; i++ {
		// request + response
		wg.Add(2)
		client.Get("/")
	}

	wg.Wait()
	const want = 10
	if reqCounter != respCounter && reqCounter != want {
		t.Errorf("want %d requests and %d responses, got %d requests and %d responses", want, want, reqCounter, respCounter)
	}
	emitter.Close()
}

// func TestInputRAW100Expect(t *testing.T) {
//
// 	wg := new(sync.WaitGroup)
// 	quit := make(chan int)

// 	fileContent, _ := ioutil.ReadFile("COMM-LICENSE")

// 	// Origing and Replay server initialization
// 	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 		defer r.Body.Close()
// 		ioutil.ReadAll(r.Body)
// 		wg.Done()
// 	}))
// 	defer origin.Close()

// 	originAddr := strings.Replace(origin.Listener.Addr().String(), "[::]", "127.0.0.1", -1)
// 	conf := RAWInputConfig{
// 		engine:        capture.EnginePcap,
// 		protocol:      ProtocolHTTP,
// 		trackResponse: true,
// 		expire:        time.Second,
// 	}
// 	input := NewRAWInput(originAddr, conf)
// 	defer input.Close()

// 	// We will use it to get content of raw HTTP request
// 	testOutput := NewTestOutput(func(data []byte) {
// 		if data[0] == RequestPayload {
// 			if strings.Contains(string(data), "Expect: 100-continue") {
// 				t.Error("request should not contain 100-continue header")
// 			}
// 		}
// 		if data[0] == ResponsePayload {
// 			if strings.Contains(string(data), "Expect: 100-continue") {
// 				t.Error("Should not contain 100-continue header")
// 			}
// 		}
// 		wg.Done()
// 	})

// 	replay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 		defer r.Body.Close()
// 		body, _ := ioutil.ReadAll(r.Body)

// 		if !bytes.Equal(body, fileContent) {
// 			buf, _ := httputil.DumpRequest(r, true)
// 			t.Error("Wrong POST body:", string(buf))
// 		}

// 		wg.Done()
// 	}))
// 	defer replay.Close()

// 	httpOutput := NewHTTPOutput(replay.URL, &HTTPOutputConfig{})

// 	plugins := &InOutPlugins{
// 		Outputs: []io.Writer{testOutput, httpOutput},
// 	}
// 	plugins.All = append(plugins.All, input, testOutput, httpOutput)

// 	emitter := NewEmitter(quit)
// 	go emitter.Start(plugins, Settings.middleware)

// 	// Origin + Response/Request Test Output + Request Http Output
// 	wg.Add(4)
// 	curl := exec.Command("curl", "http://"+originAddr, "--data-binary", "@COMM-LICENSE")
// 	err := curl.Run()
// 	if err != nil {
// 		log.Fatal(err)
// 	}

// 	wg.Wait()
// 	emitter.Close()
// }

func TestInputRAWChunkedEncoding(t *testing.T) {
	wg := new(sync.WaitGroup)
	quit := make(chan int)

	fileContent, _ := ioutil.ReadFile("README.md")

	// Origing and Replay server initialization
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		ioutil.ReadAll(r.Body)

		wg.Done()
	}))

	originAddr := strings.Replace(origin.Listener.Addr().String(), "[::]", "127.0.0.1", -1)
	conf := RAWInputConfig{
		engine:        capture.EnginePcap,
		expire:        time.Second,
		protocol:      ProtocolHTTP,
		trackResponse: true,
	}
	input := NewRAWInput(originAddr, conf)

	replay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, _ := ioutil.ReadAll(r.Body)

		if !bytes.Equal(body, fileContent) {
			buf, _ := httputil.DumpRequest(r, true)
			t.Error("Wrong POST body:", string(buf))
		}

		wg.Done()
	}))
	defer replay.Close()

	httpOutput := NewHTTPOutput(replay.URL, &HTTPOutputConfig{Debug: true})

	plugins := &InOutPlugins{
		Inputs:  []io.Reader{input},
		Outputs: []io.Writer{httpOutput},
	}
	plugins.All = append(plugins.All, input, httpOutput)

	emitter := NewEmitter(quit)
	go emitter.Start(plugins, Settings.middleware)
	wg.Add(2)

	curl := exec.Command("curl", "http://"+originAddr, "--header", "Transfer-Encoding: chunked", "--header", "Expect:", "--data-binary", "@README.md")
	err := curl.Run()
	if err != nil {
		log.Fatal(err)
	}

	wg.Wait()
	emitter.Close()
}

func BenchmarkRAWInputWithReplay(b *testing.B) {
	Settings.verbose = -1
	b.StopTimer()
	var respCounter, reqCounter, replayCounter, capturedBody uint64
	wg := &sync.WaitGroup{}
	wg.Add(b.N * 3) // reqCounter + replayCounter + respCounter

	quit := make(chan int)
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		b.Error(err)
		return
	}
	listener0, err := net.Listen("tcp", ":0")
	if err != nil {
		b.Error(err)
		return
	}

	origin := http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("ab"))
		}),
	}
	go origin.Serve(listener)
	defer origin.Close()
	originAddr := strings.Replace(listener.Addr().String(), "[::]", "127.0.0.1", -1)

	replay := http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddUint64(&replayCounter, 1)
			data, _ := ioutil.ReadAll(r.Body)
			capturedBody += uint64(len(data))
			wg.Done()
		}),
	}
	go replay.Serve(listener0)
	defer replay.Close()
	replayAddr := strings.Replace(listener0.Addr().String(), "[::]", "127.0.0.1", -1)

	conf := RAWInputConfig{
		engine:        capture.EnginePcap,
		expire:        testRawExpire,
		protocol:      ProtocolHTTP,
		trackResponse: true,
	}
	input := NewRAWInput(originAddr, conf)

	testOutput := NewTestOutput(func(data []byte) {
		if data[0] == '1' {
			atomic.AddUint64(&reqCounter, 1)
		} else {
			atomic.AddUint64(&respCounter, 1)
		}
		wg.Done()
	})
	httpOutput := NewHTTPOutput(replayAddr, &HTTPOutputConfig{Debug: false})

	plugins := &InOutPlugins{
		Inputs:  []io.Reader{input},
		Outputs: []io.Writer{testOutput, httpOutput},
	}

	emitter := NewEmitter(quit)
	go emitter.Start(plugins, Settings.middleware)
	b.StartTimer()
	now := time.Now()
	for i := 0; i < b.N; i++ {
		if i&1 == 0 {
			go http.Get("http://" + originAddr)
			continue
		}
		var buf [5 << 20]byte
		buf[5<<20-1] = 'a'
		go http.Post("http://"+originAddr, "text/html", bytes.NewBuffer(buf[:]))
	}

	wg.Wait()
	b.Logf("Captured %d Requests, %d Responses, %d Replayed, %d Bytes in %s\n", reqCounter, respCounter, replayCounter, capturedBody, time.Since(now))
	emitter.Close()
}
