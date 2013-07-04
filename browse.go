package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
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

// func browse(url, data string) error {
// 	log.Printf("visit: %s\n", url)
// 	// make sure we have a valid data dir
// 	if _, err := os.Stat(data); err != nil {
// 		if err := os.MkdirAll(data, os.ModePerm); err != nil {
// 			return err
// 		}
// 	}

// 	// open stderr
// 	stderr, err := os.Create(filepath.Join(data, "stderr"))
// 	if err != nil {
// 		return err
// 	}
// 	defer stderr.Close()

// 	// open stdout
// 	stdout, err := os.Create(filepath.Join(data, "stdout"))
// 	if err != nil {
// 		return err
// 	}
// 	defer stdout.Close()

// 	// build the command & execute it
// 	c := exec.Command(phantomJsPath,
// 		fmt.Sprintf("--cookies-file=%s", filepath.Join(data, "cookies")),
// 		fmt.Sprintf("--local-storage-path=%s", filepath.Join(data, "local-storage")),
// 		"visit.js",
// 		url,
// 		filepath.Join(data, "cookies.json"))

// 	c.Stderr = stderr
// 	c.Stdout = stdout

// 	return c.Run()
// }

type visitor struct {
	data string
}

func (v *visitor) browse(url string) error {
	log.Printf("visit: %s\n", url)
	// make sure we have a data dir
	if _, err := os.Stat(v.data); err != nil {
		if err := os.MkdirAll(v.data, os.ModePerm); err != nil {
			return err
		}
	}

	// open stderr
	stderr, err := os.Create(filepath.Join(v.data, "stderr"))
	if err != nil {
		return err
	}
	defer stderr.Close()

	// open stdout
	stdout, err := os.Create(filepath.Join(v.data, "stdout"))
	if err != nil {
		return err
	}
	defer stdout.Close()

	cf, sf := v.files()
	c := exec.Command(phantomJsPath,
		fmt.Sprintf("--cookies-file=%s", cf),
		fmt.Sprintf("--local-storage-path=%s", sf),
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
	if _, err := os.Stat(c); err == nil {
		if err := os.Remove(c); err != nil {
			return err
		}
	}

	if _, err := os.Stat(s); err == nil {
		if err := os.Remove(s); err != nil {
			return err
		}
	}

	return nil
}

func browser(req <-chan *request, rsp chan<- error) {
	var v visitor
	for r := range req {
		v.data = filepath.Join("data", fmt.Sprintf("%04d", r.ix))

		var err error

		for i := 0; i < 5; i++ {
			err = v.browse(r.url)
			if err != nil {
				log.Printf("failure: %s (retry: %d)", r.url, i)
				if err := v.clean(); err != nil {
					rsp <- err
					return
				}
				log.Printf("  (%s) cleaned, retrying...", r.url)
				continue // try again
			}

			log.Printf("  (%s) success, moving on...", r.url)
			break // success
		}

		if err != nil {
			// TODO(knorton): write failure into the data directory and move on.
			rsp <- err
			return
		}

		log.Printf("success: %s", r.url)
	}

	rsp <- nil
}

func sample(sites []string, n int) []string {
	if n > len(sites) {
		n = len(sites)
	}

	p := rand.Perm(n)
	s := make([]string, n)
	for i := 0; i < n; i++ {
		s[i] = sites[p[i]]
	}

	return s
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	N := 8

	flagTrial := flag.Int("trial", 0, "")

	flag.Parse()

	sites, err := LoadSites()
	if err != nil {
		panic(err)
	}

	if *flagTrial > 0 {
		sites = sample(sites, *flagTrial)
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
