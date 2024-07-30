package main

//go:generate go run generators/tls.go

import (
	"crypto/tls"
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

var singleton *Bolton

var (
	goServPort   = kingpin.Flag("port", "port").Default(":8100").String()
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
	mbdb.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucket([]byte("MyBucket"))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		return nil
	})
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
			//fmt.Printf("Allocated ID %s\n", uri)
			return nil
		},
		get: func(uri string) []byte {
			bolton := GetBoltInstance()
			var res []byte
			//fmt.Println(uri)
			bolton.bdb.View(func(tx *bolt.Tx) error {
				b := tx.Bucket([]byte("MyBucket"))
				v := b.Get([]byte(uri))
				res = v
				if len(v) > 0 {
					//fmt.Printf("Accessed: %s\n", v)
				}
				return nil
			})
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

func serveStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "css" {
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, "css/style.css")
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
		bolton.update(upath)
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
	wd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	w.Header().Set("Cache-Control", "no-cache")
	tmpl := template.Must(template.ParseFiles(wd + "/templates/layout.html"))
	pagedata := LinkPageData{
		PageTitle: "test",
		Links:     populateLinks(name, upath),
	}
	tmpl.Execute(w, pagedata)
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
	mux.Handle("/", http.StripPrefix("/", serveStatic(logRequests(finalHandler))))

	cfg := TLSConfig
	srv := &http.Server{
		Addr:         *goServPort,
		Handler:      mux,
		TLSConfig:    cfg,
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler), 0),
	}
	fmt.Printf("Listening on %s\n", *goServPort)
	log.Fatal(srv.ListenAndServeTLS(*goServTlsCrt, *goServTlsKey))
}
