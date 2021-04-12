package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"
	"time"
)

type config struct {
	URL  string // Repository URL
	Path string // Docker context
	File string // Dockerfile
}

// m is used to lock JSON files
var m sync.RWMutex

func panicIfNot(target, err error) {
	if err != nil && err != target && !errors.Is(err, target) {
		panic(err)
	}
}

func readJSON(file string, v interface{}) {
	m.RLock()
	defer m.RUnlock()
	b, err := os.ReadFile(file)
	panicIfNot(nil, err)
	panicIfNot(nil, json.Unmarshal(b, v))
}

func webhooks() (webhooksToCall []string) {
	readJSON("webhooks.json", &webhooksToCall)
	return
}

func imagesToBuild() (images map[string]config) {
	readJSON("build.json", &images)
	return
}

func imagesToMirror() (images []string) {
	readJSON("mirror.json", &images)
	return
}

func run(name string, args ...string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	log.Println(name, args)
	b, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err == nil {
		return true
	}
	if _, ok := err.(*exec.ExitError); !ok {
		panic(err)
	}
	log.Println(name, args, string(b), err, ctx.Err())
	return false
}

func mirror() {
	for {
		start := time.Now()
		for _, image := range imagesToMirror() {
			if run("docker", "pull", image) &&
				run("docker", "tag", image, "docker.01-edu.org/"+image) {
				run("docker", "push", "docker.01-edu.org/"+image)
			}
		}
		time.Sleep(time.Hour - time.Since(start))
	}
}

func build(URLToBuild <-chan string) {
	for URL := range URLToBuild {
		dir := path.Join("repositories", strings.TrimSuffix(path.Base(URL), ".git"))
		if _, err := os.Stat(dir); os.IsNotExist(err) && !run("git", "clone", URL, dir) {
			continue
		} else if !run("git", "-C", dir, "pull", "--ff-only") {
			continue
		}
		for image, cfg := range imagesToBuild() {
			if URL == cfg.URL {
				dir := path.Join(dir, cfg.Path)
				file := path.Join(dir, cfg.File)
				if run("docker", "build", "--tag", "docker.01-edu.org/"+image, "--file", file, dir) &&
					run("docker", "push", "docker.01-edu.org/"+image) {
					for _, webhook := range webhooks() {
						req, err := http.NewRequest("PUT", webhook, nil)
						panicIfNot(nil, err)
						resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
						if err != nil {
							log.Println(webhook, err)
						} else {
							resp.Body.Close()
						}
					}
				}
			}
		}
	}
}

func handleWebhook(URLToBuild chan<- string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost { // GitHub webhooks are POST requests
			return
		}
		var payload struct {
			Ref        string
			Repository struct {
				URL string `json:"ssh_url"`
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			log.Println("Cannot decode webhook", err)
		} else if payload.Ref != "refs/heads/master" && payload.Ref != "refs/heads/main" {
			log.Println("Branch is not master/main", payload.Ref)
		} else if payload.Repository.URL == "git@github.com:01-edu/registry.git" {
			m.Lock()
			run("git", "pull", "--ff-only")
			m.Unlock()
		} else if payload.Repository.URL != "" {
			URLToBuild <- payload.Repository.URL
		}
	}
}

func main() {
	go mirror()
	URLToBuild := make(chan string)
	go build(URLToBuild)
	go func() { // first build
		URL := map[string]struct{}{}
		for _, cfg := range imagesToBuild() {
			URL[cfg.URL] = struct{}{}
		}
		for URL := range URL {
			URLToBuild <- URL
		}
	}()
	http.HandleFunc("/", handleWebhook(URLToBuild))
	srv := http.Server{
		Addr:         ":8081",
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}
	panicIfNot(http.ErrServerClosed, srv.ListenAndServe())
}
