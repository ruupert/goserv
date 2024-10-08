package main

//go:generate go run generators/tls.go

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"text/template"
	"time"
	"unicode/utf8"

	"github.com/grafana/pyroscope-go"

	bolt "go.etcd.io/bbolt"
)

type Link struct {
	Name string
	Href string
	Date int64
	Tick string
}

type LinkPageData struct {
	PageTitle string
	Links     []Link
}

type boltUpdateType func(uri string) error
type boltGetType func(uri string) []byte
type boltDumpType func(uri string) error
type arrayFlags []string

func (i *arrayFlags) String() string {
	return fmt.Sprintf("%v", *i)
}

func (i *arrayFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}

func (i *arrayFlags) Contains(value string) bool {
	for _, item := range *i {
		if item == value {
			return true
		}
	}
	return false
}

type Bolton struct {
	bdb    *bolt.DB
	update boltUpdateType
	get    boltGetType
	dump   boltDumpType
}

//go:embed assets/css/style.css
//go:embed assets/templates/layout.html
var embedded embed.FS

var singleton *Bolton

var (
	goServPort            string
	goServAddr            string
	goServDir             string
	goServTlsCrt          string
	goServTlsKey          string
	goServBoltDB          string
	goIgnoreFiles         arrayFlags
	goServePyroscope      string
	goServePyroscopeName  string
	goServePyroscopePort  string
	goServePyroscopeProto string
)

func init() {

	flag.StringVar(&goServPort, "port", "8100", "port to bind")
	flag.StringVar(&goServAddr, "addr", "0.0.0.0", "addr to use")
	flag.StringVar(&goServDir, "dir", ".", "dir to serve")
	flag.StringVar(&goServTlsCrt, "crt", "tls.crt", "crtfile")
	flag.StringVar(&goServTlsKey, "key", "tls.key", "keyfile")
	flag.StringVar(&goServBoltDB, "db", "bolt.db", "db file")
	flag.Var(&goIgnoreFiles, "ignore", "repeatable, -ignore fname1 -ignore fname2")
	flag.StringVar(&goServePyroscope, "pyroscope", "", "Pyroscope pplication name")
	flag.StringVar(&goServePyroscopeName, "pyroscope app name", "", "Pyroscope proto")
	flag.StringVar(&goServePyroscopePort, "pyroscope port", "4040", "Pyroscope port")
	flag.StringVar(&goServePyroscopeProto, "pyroscope proto", "http", "Pyroscope proto")

	if !strings.HasSuffix(os.Args[0], ".test") {
		flag.Parse()
	} else {
		goServBoltDB = "testdata/bolt.db"
	}

	fmt.Println("Initializing")
	mbdb, err := bolt.Open(goServBoltDB, 0600, nil)
	if err != nil {
		log.Fatal(err)
	}
	err = mbdb.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucket([]byte("MyBucket"))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		return nil
	})
	if err != nil {
		fmt.Println(err)
	}
	singleton = &Bolton{
		bdb: mbdb,
		update: func(uri string) error {
			bolton := GetBoltInstance()
			berr := bolton.bdb.Update(func(tx *bolt.Tx) error {
				b := tx.Bucket([]byte("MyBucket"))
				err := b.Put([]byte(uri), []byte(time.Now().Format(time.RFC3339)))
				return err
			})
			if berr != nil {
				fmt.Println(berr)
				return berr
			}
			return nil
		},
		get: func(uri string) []byte {
			bolton := GetBoltInstance()
			var res []byte
			err := bolton.bdb.View(func(tx *bolt.Tx) error {
				b := tx.Bucket([]byte("MyBucket"))
				v := b.Get([]byte(uri))
				res = v
				return nil
			})
			if err != nil {
				fmt.Println(err)
			}
			if len(res) > 0 {
				return res
			} else {
				return []byte("")
			}
		},
		dump: func(uri string) error {
			bolton := GetBoltInstance()
			errr := bolton.bdb.View(func(tx *bolt.Tx) error {
				b := tx.Bucket([]byte("MyBucket"))
				c := b.Cursor()
				for k, v := c.First(); k != nil; k, v = c.Next() {
					fmt.Printf("key=%s, value=%s\n", k, v)
				}
				return nil
			})
			return errr
		},
	}
}

func initPyroscope(addr string, proto string, port string, name string) {
	runtime.SetMutexProfileFraction(5)
	runtime.SetBlockProfileRate(5)
	_, err := pyroscope.Start(pyroscope.Config{
		ApplicationName: name,
		ServerAddress:   fmt.Sprintf("%s://%s:%s", proto, addr, port),
		Logger:          nil, // pyroscope.StandardLogger,
		ProfileTypes: []pyroscope.ProfileType{
			pyroscope.ProfileCPU,
			pyroscope.ProfileAllocObjects,
			pyroscope.ProfileAllocSpace,
			pyroscope.ProfileInuseObjects,
			pyroscope.ProfileInuseSpace,
			pyroscope.ProfileGoroutines,
			pyroscope.ProfileMutexCount,
			pyroscope.ProfileMutexDuration,
			pyroscope.ProfileBlockCount,
			pyroscope.ProfileBlockDuration,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}

func getPyroscopeAppName() string {
	if goServePyroscopeName != "" {
		return goServePyroscopeName
	}
	var hostname, runuser string
	var err error
	hostname, err = os.Hostname()
	if err != nil {
		hostname = "undef"
	}
	user, err := user.Current()
	if err != nil {
		runuser = "anonymous"
	} else {
		runuser = user.Username
	}
	return fmt.Sprintf("goserv.%s.%s", hostname, runuser)
}

func GetBoltInstance() *Bolton {
	return singleton
}

func filterRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Connection", "close")
		if r.Method == "GET" {
			next.ServeHTTP(w, r)
		} else {
			http.Error(w, "Invalid request", http.StatusMethodNotAllowed)
		}
	})
}

func serveStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "css" {
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFileFS(w, r, embedded, "assets/css/style.css")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bolton := GetBoltInstance()
		upath := path.Clean(r.URL.Path)
		//fmt.Println("Upath is: " + upath)
		err := bolton.update(upath)
		if err != nil {
			fmt.Println(err)
		}
		next.ServeHTTP(w, r)
	})
}

func handlePath(w http.ResponseWriter, r *http.Request) {
	upath := path.Clean(r.URL.Path)
	name := filepath.Join(goServDir, upath)
	fh, err := os.Stat(name)
	if err != nil {
		fmt.Printf("File %s error: %s ", name, err)
		return
	}
	if !fh.IsDir() {
		// terrible but works
		if !goIgnoreFiles.Contains(name) {
			serveFile(w, r, name)
		}
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	tmpl := template.Must(template.ParseFS(embedded, "assets/templates/layout.html"))
	pagedata := LinkPageData{
		PageTitle: "test",
		Links:     populateLinks(name, upath),
	}
	err = tmpl.Execute(w, pagedata)
	if err != nil {
		fmt.Println(err)
	}
}

func populateLinks(name string, upath string) []Link {
	files, err := os.ReadDir(name)
	if err != nil {
		fmt.Println(err)
	}
	var links []Link
	for _, file := range files {
		if !goIgnoreFiles.Contains(file.Name()) { // terrible but works
			var link Link
			link.Name = file.Name()
			finfo, err := file.Info()
			if err != nil {
				fmt.Println(err)
			}
			link.Date = finfo.ModTime().Unix()
			link.Href = getHref(file, upath)
			link.Tick = getTick(upath, file.Name())
			links = append(links, link)
		}
	}
	if upath == "." {
		sort.Slice(links, func(i, j int) bool {
			return links[i].Date >= links[j].Date
		})
	} else {
		sort.Slice(links, func(i, j int) bool {
			return links[i].Name <= links[j].Name
		})
	}
	return links
}

func getHref(file fs.DirEntry, upath string) string {
	var res string
	if upath == "." {
		res = url.PathEscape(file.Name())
	} else {
		res = url.PathEscape((upath + "/" + file.Name()))
	}
	return res
}

func getTick(upath string, name string) string {
	var res string
	bolton := GetBoltInstance()
	var path []byte
	if upath == "." {
		path = bolton.get(name)
	} else {
		path = bolton.get(upath + "/" + name)
	}
	_, terr := time.Parse(time.RFC3339, string(path))
	if terr != nil {
		res = ""
	} else {
		src := "\u2713\u2715"
		r, _ := utf8.DecodeRuneInString(src)
		res = string(r)
	}
	return res
}

func serveFile(w http.ResponseWriter, r *http.Request, name string) {
	fmt.Println("Serving: " + name)
	http.ServeFile(w, r, name)
}

func main() {
	if goServAddr != "" {
		initPyroscope(goServePyroscope, goServePyroscopeProto, goServePyroscopePort, getPyroscopeAppName())
	}
	mux := http.NewServeMux()
	finalHandler := http.HandlerFunc(handlePath)
	mux.Handle("/", http.StripPrefix("/", filterRequests(serveStatic(logRequests(finalHandler)))))
	srv := getTLSSrv(goServAddr, goServPort, TLSConfig, mux)
	fmt.Printf("Listening on %s\n", goServPort)
	log.Fatal(srv.ListenAndServeTLS(goServTlsCrt, goServTlsKey))
}
