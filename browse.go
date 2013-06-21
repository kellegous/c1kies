package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const (
	phantomJsPath = "phantomjs-1.8.1-macosx/bin/phantomjs"
	visitJsPath   = "visit.js"
)

type request struct {
	ix  int
	url string
}

func LoadSites() ([]string, error) {
	r, err := os.Open("sites.json")
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var s []string
	if err := json.NewDecoder(r).Decode(&s); err != nil {
		return nil, err
	}

	return s, nil
}

func browse(url, data string) error {
	log.Printf("visit: %s\n", url)
	// make sure we have a valid data dir
	if _, err := os.Stat(data); err != nil {
		if err := os.MkdirAll(data, os.ModePerm); err != nil {
			return err
		}
	}

	// open stderr
	stderr, err := os.Create(filepath.Join(data, "stderr"))
	if err != nil {
		return err
	}
	defer stderr.Close()

	// open stdout
	stdout, err := os.Create(filepath.Join(data, "stdout"))
	if err != nil {
		return err
	}
	defer stdout.Close()

	// build the command & execute it
	c := exec.Command(phantomJsPath,
		fmt.Sprintf("--cookies-file=%s", filepath.Join(data, "cookies")),
		fmt.Sprintf("--local-storage-path=%s", filepath.Join(data, "local-storage")),
		"visit.js",
		url,
		filepath.Join(data, "cookies.json"))

	c.Stderr = stderr
	c.Stdout = stdout

	return c.Run()
}

type visitor struct {
	data string
}

func (v *visitor) browse(url string) error {
	log.Printf("visit: %s\n", url)
	// make sure we have a data dir
	if _, err := os.Stat(v.data); err != nil {
		if err := os.MkdirAll(data, os.ModePerm); err != nil {
			return err
		}
	}

	// open stderr
	stderr, err := os.Create(filepath.Join(data, "stderr"))
	if err != nil {
		return err
	}
	defer stderr.Close()

	// open stdout
	stdout, err := os.Create(filepath.Join(data, "stdout"))
	if err != nil {
		return err
	}
	defer stdout.Close()

	cf, sf := v.files()
	c := exec.Command(phantomJsPath,
		fmt.Sprintf("--cookies-file=%s", cf),
		fmt.Sprintf("--local-storage-file=%s", sf),
		"visit.js",
		url,
		filepath.Join(v.data, "cookies.json"))

	c.Stderr = stderr
	c.Stdout = stdout

	return c.Run()
}

func (v *visitor) files() (string, string) {
	return filepath.Join(v.data, "cookies"), filepath.Join(v.data, "local-storage")
}

func (v *visitor) clean() error {
	c, s := v.files()
	if _, err := os.Stat(c); err != nil {
		if err := os.Remove(c); err != nil {
			return err
		}
	}

	if _, err := os.Stat(s); err != nil {
		if err := os.Remove(s); err != nil {
			return err
		}
	}

	return nil
}

func browser(req <-chan *request, rsp chan<- error) {
	for r := range req {
		// TODO(knorton): retry loop
		if err := browse(r.url, filepath.Join("data", fmt.Sprintf("%04d", r.ix))); err != nil {
			log.Printf("failure: %s\n", r.url)
			rsp <- err
			return
		}
	}

	rsp <- nil
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	N := 8

	sites, err := LoadSites()
	if err != nil {
		panic(err)
	}

	req := make(chan *request, 1000)
	rsp := make(chan error)

	// start N workers
	for i := 0; i < N; i++ {
		go browser(req, rsp)
	}

	// deliver all the requests
	for i, site := range sites {
		req <- &request{ix: i, url: site}
	}
	close(req)

	// wait on all workers to finish up or error
	for i := 0; i < N; i++ {
		if err := <-rsp; err != nil {
			panic(err)
		}
	}
}
