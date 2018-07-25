package main

import (
	"flag"
	"io"
	"net"
	"net/http"
	"encoding/json"

	log "github.com/Sirupsen/logrus"
    "gopkg.in/resty.v1"
	
)

var (
	flDockerSocket       string
	flListenAddr         string
	flCertPath           string
	flKeyPath            string
	flDebug              bool
	flInsecureSkipVerify bool
)

func init() {
	flag.StringVar(&flDockerSocket, "d", "/var/run/docker.sock", "path to the Docker socket")
	flag.StringVar(&flListenAddr, "l", ":2375", "listen address")
	flag.StringVar(&flCertPath, "cert", "", "path to certificate")
	flag.StringVar(&flKeyPath, "key", "", "path to certificate key")
	flag.BoolVar(&flDebug, "D", false, "enable debug logging")
	flag.BoolVar(&flInsecureSkipVerify, "i", false, "allow insecure communication")
}


const (
  PING = "http://auth.stackhub.co/api/ping"
)

type Response struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

func main() {
	log.Info("docker proxy")
	flag.Parse()

	if flDebug {
		log.SetLevel(log.DebugLevel)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := flDockerSocket

		resp, err := resty.R().
		SetHeader("X-Auth-Token", r.Header.Get("X-Auth-Token")).
		SetHeader("X-User-Id", r.Header.Get("X-User-Id")).
		Get(PING) 

		if err != nil {
			http.Error(w, err.Error(), 501)
			return
		}
		
		var result Response
		
		err = json.Unmarshal(resp.Body(), &result)
		if err != nil {
			http.Error(w, "Internal server error", 501)
			return
		}
		
		if result.Status == "error" {
			http.Error(w, result.Message, 401)
			return 
		}		

		var c net.Conn

		cl, err := net.Dial("unix", target)
		if err != nil {
			log.Errorf("error connecting to backend: %s", err)
			return
		}

		c = cl
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "hijack error", 500)
			return
		}
		nc, _, err := hj.Hijack()
		if err != nil {
			log.Printf("hijack error: %v", err)
			return
		}
		defer nc.Close()
		defer c.Close()

		err = r.Write(c)
		if err != nil {
			log.Printf("error copying request to target: %v", err)
			return
		}

		errc := make(chan error, 2)
		cp := func(dst io.Writer, src io.Reader) {
			_, err := io.Copy(dst, src)
			errc <- err
		}
		go cp(c, nc)
		go cp(nc, c)
		<-errc
	})

	if flCertPath != "" && flKeyPath != "" {
		log.Infof("Configuring TLS: cert=%s key=%s", flCertPath, flKeyPath)

		log.Fatal(http.ListenAndServeTLS(flListenAddr, flCertPath, flKeyPath, handler))
	} else {

		log.Fatal(http.ListenAndServe(flListenAddr, handler))
	}
}
