package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/template"

	"github.com/gogits/git"
)

type TreeServerData struct {
	RepoName    string
	BranchName  string
	ArchivePath string

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

// var REPO *git.Repository
// var MAIN *git.Tree
// var ALLENTRIES []string

const (
	hook_content = `
#!/bin/sh
## Hook created by gogitserver

git update-server-info

pkill -SIGTSTP gogitserver
	`
)

// var (
// REPOPATH    string
// REPONAME    string
// ARCHIVEPATH string
// )

type Config struct {
	Repos map[string]*Repo
	Port  int
}

type Repo struct {
	Name        string
	Path        string
	Archivepath string
	Repo        *git.Repository
	AllEntries  *[]string
	Tree        *git.Tree
	Description string
}

type JConfig struct {
	Repos []JRepo "json:repos"
	Port  int     "json:port"
}

type JRepo struct {
	Name        string "json:name"
	Path        string "json:path"
	Description string "json:description"
}

var config Config

func (repo *Repo) setupGitHook() {
	hookpath := filepath.Join(repo.Path, "hooks/post-update")
	_, err := os.Stat(hookpath)
	if err != nil {
		// log.Println(err)
		fmt.Println("\tpost-update hook not found. Creating one...")
		ioutil.WriteFile(hookpath, []byte(hook_content), 0774)
	} else {
		bytes, err := ioutil.ReadFile(hookpath)
		if err != nil {
			log.Println(err)
		}

		// compare
		if strings.Compare(hook_content, string(bytes)) != 0 {
			fmt.Println("Replacing post-update hook.")
			ioutil.WriteFile(hookpath, []byte(hook_content), 0774)
		}
	}

	updatecmd := exec.Command("git", "update-server-info")
	updatecmd.Dir = repo.Path
	_, err = updatecmd.CombinedOutput()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("\tRunning 'git update-server-info' to construct refs")

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGTSTP)
	go func() {
		for _ = range c {
			log.Println("Got signal. updating repo.")
			for _, v := range config.Repos {
				v.loadRepo()
			}
		}
	}()
}

func (repo *Repo) loadRepo() {
	r, err := git.OpenRepository(repo.Path)
	if err != nil {
		// log.Fatal(err)
		fmt.Println("Could not find git repository at '" + repo.Path + "'")
	}

	repo.Repo = r

	ci, err := r.GetCommitOfBranch("master")
	if err != nil {
		log.Fatal(err)
	}

	_, allentries := walkTree(&ci.Tree)

	repo.AllEntries = &allentries

	// MAIN = &ci.Tree
	repo.Tree = &ci.Tree

	// err = ci.CreateArchive("./archive", git.AT_ZIP)
	err = ci.CreateArchive(repo.Archivepath, git.AT_TARGZ)
	if err != nil {
		log.Println(err)
	}
}

func loadConfig() Config {
	home_dir := os.Getenv("HOME")
	configfile, err := ioutil.ReadFile(filepath.Join(home_dir, ".config/gogitserver.conf"))
	if err != nil {
		log.Fatal(err)
	}

	var jsonconfig JConfig

	err = json.Unmarshal(configfile, &jsonconfig)
	if err != nil {
		log.Fatal(err)
	}

	config = Config{
		Repos: map[string]*Repo{},
	}

	for _, r := range jsonconfig.Repos {
		archpath := r.Path + "-" + "master" + ".tar.gz"
		if strings.HasSuffix(r.Path, ".git") {
			archpath = r.Path[:len(r.Path)-4] + "-" + "master" + ".tar.gz"
		}

		repo := Repo{
			Name:        r.Name,
			Path:        r.Path,
			Description: r.Description,
			Archivepath: archpath,
		}

		// config.Repos = append(config.Repos, repo)
		config.Repos[r.Name] = &repo
	}

	config.Port = jsonconfig.Port

	if jsonconfig.Port == 0 {
		config.Port = 8080
	}

	return config
}

func main() {

	f, err := os.OpenFile("logfile.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Println(err)
	}
	defer f.Close()
	if len(os.Args) > 1 && os.Args[1] == "-d" {
		fmt.Println("Starting in debug mode")
		log.SetFlags(log.Lshortfile)
	} else {
		log.SetOutput(f)
	}

	config := loadConfig()
	for _, repo := range config.Repos {
		fmt.Println("Setting up server for " + repo.Name)
		fmt.Println("\tLocated at " + repo.Path)
		fmt.Println("\tArchive at " + repo.Archivepath)
		repo.setupGitHook()
		repo.loadRepo()
		http.HandleFunc("/"+repo.Name+"/", handleGit)
		http.HandleFunc("/clone/"+repo.Name+"/", handleClone)
		http.HandleFunc("/download/"+repo.Name+"/", handleDownload)

	}

	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/static/", handleStatic)

	fmt.Println("Listening from port " + strconv.Itoa(config.Port))

	http.ListenAndServe(":"+strconv.Itoa(config.Port), nil)
}

func getRepoFromURI(uri string) *Repo {
	name := strings.Split(uri, "/")[0]
	if repo, ok := config.Repos[name]; ok {
		return repo
	}

	return nil
}

func handleClone(rw http.ResponseWriter, req *http.Request) {
	log.Println("CLONE: ", req.URL.EscapedPath())
	repo := getRepoFromURI(req.URL.EscapedPath()[len("/clone/"):])
	if repo == nil {
		http.NotFound(rw, req)
		return
	}
	p := filepath.Join(repo.Path, req.URL.EscapedPath()[len("/clone/"+repo.Name):])
	http.ServeFile(rw, req, p)
	// log.Println(req.URL.EscapedPath()) log.Println(req.URL.Path)
}

func handleDownload(rw http.ResponseWriter, req *http.Request) {
	log.Println("DOWNLOAD: ", req.URL.EscapedPath())
	repo := getRepoFromURI(req.URL.EscapedPath()[len("/download/"):])
	if repo == nil {
		http.NotFound(rw, req)
		return
	}
	// p := filepath.Join(repo.Path, req.URL.EscapedPath()[len("/download/"+repo.Name):])
	// http.NotFound(rw, req)
	http.ServeFile(rw, req, repo.Archivepath)
	// log.Println(req.URL.EscapedPath()) log.Println(req.URL.Path)
}

func handleIndex(rw http.ResponseWriter, req *http.Request) {
	log.Println("INDEX")
	// http.Redirect(rw, req, "./"+config.Repos[0].Name, http.StatusFound)
	// http.NotFound(rw, req)
	// TODO index of all repos
	rw.Write(CreateIndexHTML())
}

func handleStatic(rw http.ResponseWriter, req *http.Request) {
	log.Println("STATIC")
	if req.RequestURI[len(req.RequestURI)-1] == '/' {
		http.NotFound(rw, req)
	} else {
		filename := req.RequestURI[8:]
		http.ServeFile(rw, req, "./static/"+filename)
	}
}

func handleGit(rw http.ResponseWriter, req *http.Request) {
	log.Println("GIT: ", req.URL.EscapedPath())
	repo := getRepoFromURI(req.URL.EscapedPath()[1 : len(req.URL.EscapedPath())-1])
	if repo == nil {
		http.NotFound(rw, req)
		return
	}

	cleanpath := req.RequestURI[len(repo.Name)+2:]

	if cleanpath == "" || !contains(repo.AllEntries, cleanpath) {
		rw.Write(CreateDirectoryHTML(repo, repo.Tree, ""))
	} else {
		path := cleanpath
		if path[len(path)-1] == '/' {
			t, err := repo.Tree.GetTreeEntryByPath(path)
			if err != nil {
				log.Println(err)
				handle404(rw, req)
			} else {
				t, err := repo.Repo.GetTree(t.Id.String())
				if err != nil {
					log.Fatal(err)
				}

				rw.Write(CreateDirectoryHTML(repo, t, path))
				// log.Println(t.)
			}

		} else {
			// requestURI == link path == path of entry
			b, err := repo.Tree.GetBlobByPath(path)
			if err != nil {
				log.Println(err)
				handle404(rw, req)
			}

			bytes := GetBlobContent(b)
			mimetype := http.DetectContentType(bytes)
			if strings.Contains(mimetype, "text/html") {
				mimetype = strings.Replace(mimetype, "text/html", "text/plain", 1)
			}

			rw.Header().Add("content-type", mimetype)
			rw.Write(bytes)
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

func CreateIndexHTML() []byte {

	templ := template.New("index.html")
	t, err := templ.ParseFiles("./templates/index.html")
	if err != nil {
		log.Fatal(err)
	}

	buf := bytes.NewBuffer(nil)

	err = t.Execute(buf, config)
	if err != nil {
		log.Fatal(err)
	}

	return buf.Bytes()
}

func CreateDirectoryHTML(repo *Repo, gittree *git.Tree, path string) []byte {

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

	hasparent := repo.Tree != gittree

	err = t.Execute(buf, TreeServerData{
		RepoName:    repo.Name,
		BranchName:  "master",
		ArchivePath: repo.Archivepath,

		Files:      tree,
		HasParent:  hasparent,
		ParentPath: parentpath,
	})
	if err != nil {
		log.Fatal(err)
	}

	return buf.Bytes()
}

func contains(list *[]string, item string) bool {
	for _, e := range *list {
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
