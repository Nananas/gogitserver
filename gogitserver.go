package main

import (
	"bufio"
	"bytes"
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
	"time"

	"gopkg.in/yaml.v2"

	"github.com/gogits/git"
)

type TreeServerData struct {
	RepoName    string
	BranchName  string
	ArchivePath string

	Files      cleanTree
	HasParent  bool
	ParentPath string

	Header string
	Footer string
}

type cleanTree []cleanBranch
type cleanBranch struct {
	IsDir    bool
	Name     string
	Branches []cleanBranch
	Path     string
	Info     string
}

const (
	hook_content = `
#!/bin/sh
## Hook created by gogitserver

git update-server-info

# a SIGTSTP signal will be captured by gogitserver to reload its repos
pkill -SIGTSTP gogitserver
	`
)

type Config struct {
	Repos  map[string]*Repo
	Port   int
	Header string
	Footer string
}

type Repo struct {
	Name        string
	Path        string
	Archivepath string
	Repo        *git.Repository
	AllEntries  *[]string
	Tree        *git.Tree
	Description string
	Header      string
	Footer      string
}

type YConfig struct {
	Repos  []YRepo "yaml:repos"
	Port   int     "yaml:port"
	Header string  "yaml:header,omitempty"
	Footer string  "yaml:footer,omitempty"
}

type YRepo struct {
	Name        string "yaml:name"
	Path        string "yaml:path"
	Description string "yaml:description,omitempty"
	Header      string "yaml:,omitempty"
	Footer      string "yaml:,omitempty"
}

var config Config

func (repo *Repo) setupGitHook() {
	hookpath := filepath.Join(repo.Path, "hooks/post-update")
	_, err := os.Stat(hookpath)
	if err != nil {
		fmt.Println("\tpost-update hook not found. Creating one...")
		ioutil.WriteFile(hookpath, []byte(hook_content), 0774)
	} else {
		bytes, err := ioutil.ReadFile(hookpath)
		if err != nil {
			log.Println(err)
		}

		// compare
		if strings.Compare(hook_content, string(bytes)) != 0 {
			t := time.Now().Format("2006-02-01-15u04")
			fmt.Println("Replacing post-update hook. Backup at post-update.backup." + t)
			os.Rename(repo.Path+"/hooks/post-update", repo.Path+"/hooks/post-update.backup."+t)
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

}

// Load repository from disk, saves all file names in the repo tree and create archive.
//
func (repo *Repo) loadRepo() {
	r, err := git.OpenRepository(repo.Path)
	if err != nil {
		fmt.Println("Could not find git repository at '" + repo.Path + "'")
	}

	repo.Repo = r

	ci, err := r.GetCommitOfBranch("master")
	if err != nil {
		log.Fatal(err)
	}

	_, allentries := walkTree(&ci.Tree)

	repo.AllEntries = &allentries

	repo.Tree = &ci.Tree

	err = ci.CreateArchive(repo.Archivepath, git.AT_TARGZ)
	if err != nil {
		log.Println(err)
	}
}

// Loads the confiuration file from the default location
// This configuration file has to exist, obviously
//
func loadConfig() Config {
	home_dir := os.Getenv("HOME")
	configfile, err := ioutil.ReadFile(filepath.Join(home_dir, ".config/gogitserver.conf"))
	if err != nil {
		log.Fatal(err)
	}

	var yamlconfig YConfig

	err = yaml.Unmarshal(configfile, &yamlconfig)
	if err != nil {
		log.Fatal(err)
	}

	config = Config{
		Repos: map[string]*Repo{},
	}

	for _, r := range yamlconfig.Repos {
		archpath := r.Path + "-" + "master" + ".tar.gz"
		if strings.HasSuffix(r.Path, ".git") {
			archpath = r.Path[:len(r.Path)-4] + "-" + "master" + ".tar.gz"
		}

		repo := Repo{
			Name:        r.Name,
			Path:        r.Path,
			Description: r.Description,
			Archivepath: archpath,
			Header:      r.Header,
			Footer:      r.Footer,
		}

		config.Repos[r.Name] = &repo
	}

	config.Port = yamlconfig.Port

	if yamlconfig.Port == 0 {
		config.Port = 8080
	}

	config.Header = yamlconfig.Header
	config.Footer = yamlconfig.Footer

	return config
}

// Will Setup the git hook, load the repository and populate the fields of the
// repo structs
//
func (c *Config) loadRepos() {

	for _, repo := range config.Repos {
		fmt.Println("Setting up repo " + repo.Name)
		fmt.Println("\tLocated at " + repo.Path)
		fmt.Println("\tArchive at " + repo.Archivepath)
		repo.setupGitHook()
		repo.loadRepo()
	}
}

// Compares the old and new config and
// renames the current post-update hook to post-update.disabled.[current-time]
// if an old repo is not in the new config
//
func disableRepoHooks(oldc, newc Config) {

	for _, repo := range oldc.Repos {
		isinnew := false
		for _, nr := range newc.Repos {
			if repo.Path == nr.Path {
				isinnew = true
			}
		}

		if !isinnew {
			t := time.Now().Format("2006-02-01-15u04")
			fmt.Println("Disabling git hook post-update for " + repo.Name + ": renaming to post-update.disabled" + t)
			os.Rename(repo.Path+"/hooks/post-update", repo.Path+"/hooks/post-update.disabled."+t)
		}
	}

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
	config.loadRepos()

	c := make(chan os.Signal, 1)
	// CUSTOM SIGNALS:
	// SIGTSTP: reload repositories
	// SIGHUP: send by Upstart: reload configuration
	signal.Notify(c, syscall.SIGTSTP, syscall.SIGHUP)
	go func() {
		for s := range c {
			switch s {
			case syscall.SIGTSTP:
				fmt.Println("[SIGTSTP]: Reloading repos.")
				for _, v := range config.Repos {
					v.loadRepo()
				}
			case syscall.SIGHUP:
				fmt.Println("[SIGHUP]: Reloading config.")

				oldconfig := config

				config = loadConfig()
				disableRepoHooks(oldconfig, config)
				config.loadRepos()

			}
		}
	}()

	http.HandleFunc("/repo/", handleGit)
	http.HandleFunc("/clone/", handleClone)
	http.HandleFunc("/download/", handleDownload)

	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/static/", handleStatic)

	fmt.Println("Listening from port " + strconv.Itoa(config.Port))

	http.ListenAndServe(":"+strconv.Itoa(config.Port), nil)
}

// Returns the repository from a string if it exists, nil otherwise
//
func getRepoFromURI(uri string) *Repo {
	name := strings.Split(uri, "/")[0]
	if repo, ok := config.Repos[name]; ok {
		return repo
	}

	return nil
}

// Git's dumb' HTTP protocol
//
func handleClone(rw http.ResponseWriter, req *http.Request) {
	log.Println("CLONE: ", req.URL.EscapedPath())
	repo := getRepoFromURI(req.URL.EscapedPath()[len("/clone/"):])
	if repo == nil {
		http.NotFound(rw, req)
		return
	}
	p := filepath.Join(repo.Path, req.URL.EscapedPath()[len("/clone/"+repo.Name):])
	http.ServeFile(rw, req, p)
}

// Basic tar.gz archive download
//
func handleDownload(rw http.ResponseWriter, req *http.Request) {
	log.Println("DOWNLOAD: ", req.URL.EscapedPath())
	repo := getRepoFromURI(req.URL.EscapedPath()[len("/download/"):])
	if repo == nil {
		http.NotFound(rw, req)
		return
	}

	http.ServeFile(rw, req, repo.Archivepath)
}

// Show list of all repositories
//
func handleIndex(rw http.ResponseWriter, req *http.Request) {
	log.Println("INDEX", req.RequestURI)
	if req.URL.EscapedPath() != "/" {
		http.NotFound(rw, req)
		return
	}

	rw.Write(CreateIndexHTML())
}

// Static file serving
//
func handleStatic(rw http.ResponseWriter, req *http.Request) {
	log.Println("STATIC")
	if req.RequestURI[len(req.RequestURI)-1] == '/' {
		http.NotFound(rw, req)
	} else {
		filename := req.RequestURI[8:]
		http.ServeFile(rw, req, "./static/"+filename)
	}
}

// Serves the file tree if a directory is requested
// Serves the file if the requested file is found in the presaved git tree (Repo.AllEnties)
// Otherwise returns a 404
//
func handleGit(rw http.ResponseWriter, req *http.Request) {
	log.Println("GIT: ", req.URL.EscapedPath())

	if req.URL.EscapedPath() == "/repo/" {
		http.Redirect(rw, req, "/", http.StatusSeeOther)
		return
	}

	repo := getRepoFromURI(req.URL.EscapedPath()[6:len(req.URL.EscapedPath())])
	if repo == nil {
		http.NotFound(rw, req)
		return
	}

	basepath := "/repo/" + repo.Name + "/"

	// make sure a / is added to the url if the base tree is served
	//
	reql := len(req.URL.EscapedPath())
	basel := len(basepath)
	if reql < basel {
		http.Redirect(rw, req, basepath, http.StatusSeeOther)
		return
	}

	// Serve Base Tree
	if reql == basel {
		rw.Write(CreateDirectoryHTML(repo, repo.Tree, ""))
		return
	}

	path := req.RequestURI[len(repo.Name)+7:]

	if !contains(repo.AllEntries, path) {
		http.Redirect(rw, req, basepath, http.StatusPermanentRedirect)
		return
	}

	log.Println(path)
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
		}

	} else {
		// requestURI == link path == path of entry
		b, err := repo.Tree.GetBlobByPath(path)
		if err != nil {
			log.Println(err)
			handle404(rw, req)
		}

		// serve HTML files as plain text
		//
		bytes := GetBlobContent(b)
		mimetype := http.DetectContentType(bytes)
		if strings.Contains(mimetype, "text/html") {
			mimetype = strings.Replace(mimetype, "text/html", "text/plain", 1)
		}

		rw.Header().Add("content-type", mimetype)
		rw.Write(bytes)
	}
}

// Basic 404 error
//
func handle404(rw http.ResponseWriter, req *http.Request) {
	http.NotFound(rw, req)
}

//
func walkTree(tree *git.Tree) (cleanTree, []string) {
	return _walkTree(tree, "")
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

// Returns the content of the blob (file) in the git repository
//
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

// Constructs the homepage html
//
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

// Constructs a directory tree html file
//
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
		Header:     repo.Header,
		Footer:     repo.Footer,
	})

	if err != nil {
		log.Fatal(err)
	}

	return buf.Bytes()
}

// helper function
//
func contains(list *[]string, item string) bool {
	for _, e := range *list {
		if item == e {
			return true
		}
	}

	return false
}

// helper function
// converts integer to human readable file size, with B, KB etc
//
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

	return strconv.Itoa(int(float64(size)/s)) + " " + suffixes[ii]
}
