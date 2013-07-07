package main

import (
	"code.google.com/p/gosqlite/sqlite"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const (
	phantomJsPath = "phantomjs-1.9.1-macosx/bin/phantomjs"
	visitJsPath   = "visit.js"

	databaseFile  = "data/db.sqlite3"
	dataDirectory = "data"
)

type request struct {
	ix  int
	url string
}

func (r *request) issue(db *sqlite.Conn, dir string) error {
	var err error
	var rsp *browseRsp

	for i := 0; i < 5; i++ {
		rsp, err = browse(dir, r.url)
		if err == nil {
			break
		}
	}

	if err != nil {
		if err := db.Exec("INSERT OR REPLACE INTO visit VALUES (?1, ?2, NULL, NULL, NULL, 0)",
			r.ix,
			r.url); err != nil {
			return err
		}
	} else {
		if err := db.Exec("INSERT OR REPLACE INTO visit VALUES (?1, ?2, ?3, ?4, ?5, 1)",
			r.ix,
			r.url,
			rsp.cookies,
			rsp.stdout,
			rsp.stderr); err != nil {
			return err
		}
	}

	return nil
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

type browseRsp struct {
	stdout  string
	stderr  string
	cookies string
}

func newBrowseRsp(stdout, stderr, cookies string) (*browseRsp, error) {
	o, err := ioutil.ReadFile(stdout)
	if err != nil {
		return nil, err
	}

	e, err := ioutil.ReadFile(stderr)
	if err != nil {
		return nil, err
	}

	c, err := ioutil.ReadFile(cookies)
	if err != nil {
		return nil, err
	}

	return &browseRsp{
		stdout:  string(o),
		stderr:  string(e),
		cookies: string(c),
	}, nil
}

func clean(dir string) error {
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		if err := os.Remove(filepath.Join(dir, file.Name())); err != nil {
			return err
		}
	}

	return nil
}

func browse(data, url string) (*browseRsp, error) {
	fmt.Printf("url: %s, data: %s\n", url, data)
	if err := clean(data); err != nil {
		return nil, err
	}

	errFile := filepath.Join(data, "stderr")
	outFile := filepath.Join(data, "stdout")

	stderr, err := os.Create(errFile)
	if err != nil {
		return nil, err
	}
	defer stderr.Close()

	stdout, err := os.Create(outFile)
	if err != nil {
		return nil, err
	}
	defer stdout.Close()

	cookiesFile := filepath.Join(data, "cookies")
	storageFile := filepath.Join(data, "storage")
	cookiesJson := filepath.Join(data, "cookies.json")

	c := exec.Command(phantomJsPath,
		fmt.Sprintf("--cookies-file=%s", cookiesFile),
		fmt.Sprintf("--local-storage-path=%s", storageFile),
		"visit.js",
		url,
		cookiesJson)
	c.Stderr = stderr
	c.Stdout = stdout

	if err := c.Run(); err != nil {
		return nil, err
	}

	stderr.Close()
	stdout.Close()

	return newBrowseRsp(outFile, errFile, cookiesJson)
}

type worker struct {
	req chan *request
	rsp chan error
	n   int
}

func (w *worker) submit(r *request) {
	w.req <- r
}

func (w *worker) close() {
	close(w.req)
}

func (w *worker) wait() error {
	for i := 0; i < w.n; i++ {
		if err := <-w.rsp; err != nil {
			return err
		}
	}
	return nil
}

func startWorker(db *sqlite.Conn, data string, n int) *worker {
	req := make(chan *request, 1000)
	rsp := make(chan error)
	w := worker{
		req: req,
		rsp: rsp,
		n:   n,
	}

	for i := 0; i < n; i++ {
		go func() {
			for r := range req {
				data := filepath.Join(data, fmt.Sprintf("%04d", r.ix))
				if err := ensureDir(data); err != nil {
					rsp <- err
					return
				}

				if err := r.issue(db, data); err != nil {
					rsp <- err
					return
				}
			}

			rsp <- nil
		}()
	}

	return &w
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

func ensureDir(dir string) error {
	if _, err := os.Stat(dir); err != nil {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			return err
		}
	}
	return nil
}

func openDatabase(dbfile string) (*sqlite.Conn, error) {
	db, err := sqlite.Open(dbfile)
	if err != nil {
		return nil, err
	}

	if db.Exec(`CREATE TABLE IF NOT EXISTS visit (
								id INTEGER PRIMARY KEY,
								url			VARCHAR(255),
								cookies TEXT,
								stdout TEXT,
								stderr TEXT,
								success INTEGER);`); err != nil {
		return nil, err
	}

	return db, nil
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	flagTrial := flag.Int("trial", 0, "")

	flag.Parse()

	if err := ensureDir(dataDirectory); err != nil {
		panic(err)
	}

	db, err := openDatabase(databaseFile)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	sites, err := LoadSites()
	if err != nil {
		panic(err)
	}

	if *flagTrial > 0 {
		sites = sample(sites, *flagTrial)
	}

	w := startWorker(db, "data", 8)
	for i, site := range sites {
		w.submit(&request{ix: i, url: site})
	}
	w.close()

	if err := w.wait(); err != nil {
		panic(err)
	}
}
