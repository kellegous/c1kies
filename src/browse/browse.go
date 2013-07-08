package main

import (
	"code.google.com/p/gosqlite/sqlite"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const (
	databaseFile  = "db.sqlite3"
	sitesJsonFile = "sites.json"
)

var rootPath string
var phantomJsPath string
var visitJsPath string

func setupPaths(root string) error {
	var err error
	if root == "" {
		_, file, _, ok := runtime.Caller(0)
		if !ok {
			return errors.New("unable to determine src directory automatically")
		}

		file = filepath.Join(filepath.Dir(file), "../..")

		root, err = filepath.Abs(file)
		if err != nil {
			return err
		}
	} else {
		root, err = filepath.Abs(root)
		if err != nil {
			return err
		}
	}

	rootPath = root
	visitJsPath = filepath.Join(root, "src/visit.js")
	deps := filepath.Join(root, "deps")
	switch runtime.GOOS {
	case "linux":
		phantomJsPath = filepath.Join(deps, "phantomjs-1.9.1-linux-x86_64/bin/phantomjs")
	case "darwin":
		phantomJsPath = filepath.Join(deps, "phantomjs-1.9.1-macosx/bin/phantomjs")
	default:
		return errors.New(fmt.Sprint("platform unsupported: %s", runtime.GOOS))
	}

	return nil
}

type request struct {
	rank int
	url  string
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
		if err := db.Exec("UPDATE visit SET success=0 WHERE url=?1", r.url); err != nil {
			return err
		}
	} else {
		if err := db.Exec("UPDATE visit SET success=1, cookies=?1, stdout=?2, stderr=?3 WHERE url = ?4",
			rsp.cookies,
			rsp.stdout,
			rsp.stderr,
			r.url); err != nil {
			return err
		}
	}

	return nil
}

func loadSites(file string) ([]string, error) {
	r, err := os.Open(file)
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
		visitJsPath,
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

func (w *worker) wait() error {
	close(w.req)
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
				data := filepath.Join(data, fmt.Sprintf("%04d", r.rank))
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

	sites, err := loadSites(filepath.Join(rootPath, sitesJsonFile))
	if err != nil {
		return nil, err
	}

	if db.Exec(`CREATE TABLE IF NOT EXISTS visit (
								url			VARCHAR(255) PRIMARY KEY,
								rank		INTEGER,
								cookies TEXT,
								stdout  TEXT,
								stderr  TEXT,
								success INTEGER);`); err != nil {
		return nil, err
	}

	for i, site := range sites {
		s, err := db.Prepare("SELECT url FROM visit WHERE url=?")
		if err != nil {
			return nil, err
		}

		if err := s.Exec(site); err != nil {
			return nil, err
		}

		if !s.Next() {
			if err := db.Exec("INSERT INTO visit (url, rank, success) VALUES(?1, ?2, 0)", site, i+1); err != nil {
				return nil, err
			}
		}
	}

	return db, nil
}

func visitsNeeded(db *sqlite.Conn) ([]*request, error) {
	var reqs []*request
	s, err := db.Prepare("SELECT url, rank FROM visit WHERE NOT success=1")
	if err != nil {
		return nil, err
	}

	if err := s.Exec(); err != nil {
		return nil, err
	}

	var url string
	var rank int
	for s.Next() {
		if err := s.Scan(&url, &rank); err != nil {
			return nil, err
		}

		reqs = append(reqs, &request{url: url, rank: rank})
	}

	return reqs, nil
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	flagRoot := flag.String("root", "", "the root of the source folder")
	flagStopAfter := flag.Int("stop-after", -1, "stop after collecting data on N sites")
	flagDataDir := flag.String("data-dir", "data", "the path to be used as a data directory")
	flagWorkers := flag.Int("workers", 4, "number of workers")

	flagReport := flag.Bool("report", false, "")

	flag.Parse()

	if err := setupPaths(*flagRoot); err != nil {
		panic(err)
	}

	if err := ensureDir(*flagDataDir); err != nil {
		panic(err)
	}

	db, err := openDatabase(filepath.Join(*flagDataDir, databaseFile))
	if err != nil {
		panic(err)
	}
	defer db.Close()

	reqs, err := visitsNeeded(db)
	if err != nil {
		panic(err)
	}

	if *flagReport {
		for _, req := range reqs {
			fmt.Printf("%s\n", req.url)
		}
		fmt.Printf("%d sites need visiting.\n", len(reqs))
		return
	}

	w := startWorker(db, *flagDataDir, *flagWorkers)
	for i, req := range reqs {
		if *flagStopAfter >= 0 && i >= *flagStopAfter {
			break
		}
		w.submit(req)
	}

	if err := w.wait(); err != nil {
		panic(err)
	}
}
