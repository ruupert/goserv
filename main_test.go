package main

import (
	"fmt"
	"log"
	"os"
	"testing"
)

/*
	func TestMainFunc(t *testing.T) {
		os.Args = append(os.Args, "-db testdata/bolt.db")
	}
*/

func teardown() {
	err := os.Remove("testdata/bolt.db")
	if err != nil {
		log.Fatal(err)
	}
}

func TestMain(m *testing.M) {
	code := m.Run()
	teardown()
	os.Exit(code)
}

func TestPopulateLinks(t *testing.T) {
	name := "testdata/dir"
	upath := "/"
	want := 2
	links := populateLinks(name, upath)
	if len(links) != want {
		t.Fatalf("fail")
	}
}

func TestPopulateLinkNames(t *testing.T) {
	name := "testdata/dir"
	upath := "/"
	var tests = []struct {
		name string
		href string
	}{
		{"file1.txt", "%2F%2Ffile1.txt"},
		{"file2.txt", "%2F%2Ffile2.txt"},
	}
	links := populateLinks(name, upath)
	for _, tt := range tests {
		testname := fmt.Sprintf("%s,%s", tt.name, tt.href)
		t.Run(testname, func(t *testing.T) {
			ans := tt.name
			res := "nope"
			for _, v := range links {
				if v.Name == ans {
					res = v.Name
				}
			}
			if ans != res {
				t.Error("fail")
			}
		})
	}
}
