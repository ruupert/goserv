package main

//go:generate go run generators/tls.go

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"text/template"
	"time"
	"unicode/utf8"

	"github.com/alecthomas/kingpin/v2"
	bolt "go.etcd.io/bbolt"
)

type Link struct {
	Name string
	Href string
	Date string
}

type LinkPageData struct {
	PageTitle string
	Links     []Link
}

type boltUpdateType func(uri string) error
type boltGetType func(uri string) []byte
type boltDumpType func(uri string) error

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
	goServPort   = kingpin.Flag("port", "port").Default("8100").String()
	goServAddr   = kingpin.Flag("addr", "addr").Default("0.0.0.0").String()
	goServDir    = kingpin.Flag("dir", "dir").Default(".").String()
	goServTlsCrt = kingpin.Flag("crt", "crtfile").Default("tls.crt").String()
	goServTlsKey = kingpin.Flag("key", "keyfile").Default("tls.key").String()
)

func init() {
	fmt.Println("Initializing")
	mbdb, err := bolt.Open("bolt.db", 0600, nil)
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
	name := filepath.Join(*goServDir, upath)
	fh, err := os.Stat(name)
	if err != nil {
		fmt.Printf("File %s error: %s ", name, err)
		return
	}
	if !fh.IsDir() {
		serveFile(w, r, name)
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
		var link Link
		link.Name = file.Name()
		link.Date = getDate(upath, file.Name())
		link.Href = getHref(file, upath)
		links = append(links, link)
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

// does not get the date but instead returns a tick char for watched
func getDate(upath string, name string) string {
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
		// res = dtime.Format(time.RFC3339)
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

	kingpin.Parse()
	mux := http.NewServeMux()
	finalHandler := http.HandlerFunc(handlePath)
	mux.Handle("/", http.StripPrefix("/", filterRequests(serveStatic(logRequests(finalHandler)))))

	srv := getTLSSrv(*goServAddr, *goServPort, TLSConfig, mux)
	fmt.Printf("Listening on %s\n", *goServPort)
	log.Fatal(srv.ListenAndServeTLS(*goServTlsCrt, *goServTlsKey))
}
