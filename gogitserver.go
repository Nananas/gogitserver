package main

import (
	"bufio"
	"bytes"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/gogits/git"
)

type TreeServerData struct {
	RepoName   string
	BranchName string

	Files      cleanTree
	HasParent  bool
	ParentPath string
}

type cleanTree []cleanBranch
type cleanBranch struct {
	IsDir    bool
	Name     string
	Branches []cleanBranch
	Path     string
	Info     string
}

var REPO *git.Repository
var MAIN *git.Tree
var ALLENTRIES []string

const (
	hook_content = `
	#!/bin/sh
	## Hook created by gogitserver
	pkill -SIGTSTP gogitserver
	`

	REPOPATH             = "./gitserver.git"
	REPONAME             = "gitserver.git"
	REPOPUBLICACCESSPATH = "./gitserver.git"
	ARCHIVEPATH          = "./gitserver.git/archive.tar.gz"
)

func setupGitHook() {
	hookpath := filepath.Join(REPOPATH, "hooks/post-update")
	_, err := os.Stat(hookpath)
	if err != nil {
		// log.Println(err)
		fmt.Println("post-update hook not found. Creating one...")
		ioutil.WriteFile(hookpath, []byte(hook_content), 0774)
	} else {
		bytes, err := ioutil.ReadFile(hookpath)
		if err != nil {
			log.Println(err)
		}

		// compare
		if strings.Compare(hook_content, string(bytes)) != 0 {
			ioutil.WriteFile(hookpath, []byte(hook_content), 0774)
		}
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGTSTP)
	go func() {
		for _ = range c {
			log.Println("Got signal. updating repo.")
			loadRepo()
		}
	}()
}

func loadRepo() {
	r, err := git.OpenRepository(REPOPATH)
	if err != nil {
		log.Fatal(err)
	}

	REPO = r

	ci, err := REPO.GetCommitOfBranch("master")
	if err != nil {
		log.Fatal(err)
	}

	_, ALLENTRIES = walkTree(&ci.Tree)

	sort.Strings(ALLENTRIES)

	MAIN = &ci.Tree

	// err = ci.CreateArchive("./archive", git.AT_ZIP)
	err = ci.CreateArchive(ARCHIVEPATH, git.AT_TARGZ)
	if err != nil {
		log.Println(err)
	}

}

func main() {

	log.SetFlags(log.Lshortfile)

	setupGitHook()
	loadRepo()

	// branches, err := REPO.GetBranches()
	// if err != nil {
	// log.Fatal(err)
	// }
	// log.Println("Branches: ", branches)
	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/"+REPONAME+"/", handleGit)
	http.HandleFunc("/git/"+REPONAME+"/", handleClone)
	http.HandleFunc("/static/", handleStatic)
	http.ListenAndServe(":8080", nil)
}

func handleClone(rw http.ResponseWriter, req *http.Request) {
	p := req.URL.EscapedPath()[len("/git"+REPOPUBLICACCESSPATH):]
	log.Println("CLONE: ", p)
	// log.Println(req.URL.EscapedPath()) log.Println(req.URL.Path)
	http.ServeFile(rw, req, filepath.Join(REPOPUBLICACCESSPATH, p))
}

func handleIndex(rw http.ResponseWriter, req *http.Request) {
	// log.Println("INDEX")
	http.Redirect(rw, req, "./"+REPONAME, http.StatusFound)
}

func handleStatic(rw http.ResponseWriter, req *http.Request) {
	// log.Println("STATIC")
	if req.RequestURI[len(req.RequestURI)-1] == '/' {
		http.NotFound(rw, req)
	} else {
		filename := req.RequestURI[8:]
		http.ServeFile(rw, req, "./static/"+filename)
	}
}

func handleGit(rw http.ResponseWriter, req *http.Request) {
	// log.Println("GIT")
	cleanpath := req.RequestURI[len(REPONAME)+2:]

	if cleanpath == "" || !contains(ALLENTRIES, cleanpath) {
		rw.Write(CreateDirectoryHTML(MAIN, ""))
	} else if req.RequestURI == "/favicon.ico" {
		// TODO
		handle404(rw, req)
	} else {
		path := cleanpath
		if path[len(path)-1] == '/' {

			t, err := MAIN.GetTreeEntryByPath(path)
			if err != nil {
				log.Println(err)
				handle404(rw, req)
			} else {
				t, err := REPO.GetTree(t.Id.String())
				if err != nil {
					log.Fatal(err)
				}

				rw.Write(CreateDirectoryHTML(t, path))
				// log.Println(t.)
			}

		} else {
			// requestURI == link path == path of entry
			b, err := MAIN.GetBlobByPath(path)
			if err != nil {
				log.Println(err)
				handle404(rw, req)
			}

			rw.Write(GetBlobContent(b))
		}
	}
}

func handle404(rw http.ResponseWriter, req *http.Request) {
	http.NotFound(rw, req)
}

func walkTree(tree *git.Tree) (cleanTree, []string) {
	// entries := tree.ListEntries()

	// clean := cleanTree{}
	// clean, allentries := _walkTree(tree, "")
	return _walkTree(tree, "")

	// clean _walkTree(tree, "")

	// allentries := []string{}

	// for _, e := range entries {

	// 	if e.IsDir() {

	// 		t, err := tree.SubTree(e.Name())
	// 		if err != nil {
	// 			log.Fatal(err)
	// 		}

	// 		allentries = append(allentries, e.Name()+"/")

	// 		subtree, subentries := _walkTree(t, e.Name())
	// 		allentries = append(allentries, subentries...)
	// 		clean = append(clean, cleanBranch{
	// 			IsDir:    true,
	// 			Name:     e.Name(),
	// 			Branches: subtree,
	// 			Path:     e.Name() + "/",
	// 			Info:     "-",
	// 		})

	// 	} else {

	// 		clean = append(clean, cleanBranch{
	// 			IsDir:    false,
	// 			Name:     e.Name(),
	// 			Branches: nil,
	// 			Path:     e.Name(),
	// 			Info:     toHumanReadableString(e.Size()),
	// 		})

	// 		allentries = append(allentries, e.Name())

	// 	}

	// }

	// return clean, allentries
}

func _walkTree(tree *git.Tree, path string) ([]cleanBranch, []string) {

	branches := []cleanBranch{}

	allentries := []string{}

	for _, e := range tree.ListEntries() {
		newpath := filepath.Join(path, e.Name())

		if e.IsDir() {
			t, err := tree.SubTree(e.Name())
			if err != nil {
				log.Fatal(err)
			}

			allentries = append(allentries, newpath+"/")

			subtree, subentries := _walkTree(t, newpath)
			allentries = append(allentries, subentries...)
			branches = append(branches, cleanBranch{
				IsDir:    true,
				Name:     e.Name(),
				Branches: subtree,
				Path:     newpath + "/",
				Info:     "-",
			})

		} else {

			branches = append(branches, cleanBranch{
				IsDir:    false,
				Name:     e.Name(),
				Branches: nil,
				Path:     newpath,
				Info:     toHumanReadableString(e.Size()),
			})

			allentries = append(allentries, newpath)
		}

	}

	return branches, allentries

}

func GetBlobContent(blob *git.Blob) []byte {
	r, err := blob.Data()
	if err != nil {
		log.Println(err)
	}

	rd := bufio.NewReader(r)

	b, err := ioutil.ReadAll(rd)
	if err != nil {
		log.Println(err)
	}

	return b
}

func CreateDirectoryHTML(gittree *git.Tree, path string) []byte {

	tree, _ := walkTree(gittree)

	split := strings.Split(path, "/")
	parentsplit := split[:len(split)-1]
	parentpath := "/" + strings.Join(parentsplit, "/")

	templ := template.New("tree.html")
	t, err := templ.ParseFiles("./templates/tree.html")
	if err != nil {
		log.Fatal(err)
	}

	buf := bytes.NewBuffer(nil)

	hasparent := MAIN != gittree

	err = t.Execute(buf, TreeServerData{RepoName: REPONAME, BranchName: "master", Files: tree, HasParent: hasparent, ParentPath: parentpath})
	if err != nil {
		log.Fatal(err)
	}

	return buf.Bytes()
}

func contains(list []string, item string) bool {
	for _, e := range list {
		if item == e {
			return true
		}
	}

	return false
}

func toHumanReadableString(size int64) string {
	suffixes := []string{
		" B", "KB", "MB", "GB",
	}

	if size == 0 {
		return "0  B"
	}

	b := math.Log(float64(size)) / math.Log(1024)
	ii := int(b)
	s := math.Pow(1000, float64(ii))

	// log.Println(size, b, int(float64(size)/s), suffixes[ii])
	return strconv.Itoa(int(float64(size)/s)) + " " + suffixes[ii]
}
