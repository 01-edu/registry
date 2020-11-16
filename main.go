package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type config struct {
	URL  string // Repository URL
	path string // Docker context
	file string // Dockerfile
}

var images = map[string]config{
	"lib-go":     {"git@github.com:01-edu/all.git", "lib/go", "Dockerfile"},
	"lib-js":     {"git@github.com:01-edu/all.git", "lib/js", "Dockerfile"},
	"lib-static": {"git@github.com:01-edu/all.git", "static", "Dockerfile.lib"},
	"test-dom":   {"git@github.com:01-edu/public.git", "", "dom/Dockerfile"},
	"run-go":     {"git@github.com:01-edu/public.git", "go/exam", "Dockerfile"},
	"test-go":    {"git@github.com:01-edu/public.git", "go/tests", "Dockerfile"},
	"test-js":    {"git@github.com:01-edu/public.git", "js/tests", "Dockerfile"},
	"test-sh":    {"git@github.com:01-edu/public.git", "sh/tests", "Dockerfile"},
	"subjects":   {"git@github.com:01-edu/public.git", "subjects", "Dockerfile"},
}

// the keys are URL
var updateNeeded = map[string]chan struct{}{}

func build(ctx context.Context, done chan<- struct{}) {
	var wg sync.WaitGroup
	for URL, c := range updateNeeded {
		URL := URL
		c := c
		wg.Add(1)
		go func() {
			for {
				select {
				case <-c:
				case <-ctx.Done():
					wg.Done()
					return
				}
				folder := strings.TrimSuffix(strings.TrimPrefix(URL, "git@github.com:01-edu/"), ".git")
				if _, err := os.Stat(folder); os.IsNotExist(err) {
					if b, err := exec.CommandContext(ctx, "git", "clone", URL, folder).CombinedOutput(); err != nil {
						if ctx.Err() == context.Canceled {
							wg.Done()
							return
						}
						log.Println("could not clone", URL, err, ctx.Err(), string(b))
						continue
					}
				}
				if b, err := exec.CommandContext(ctx, "git", "-C", folder, "pull", "--ff-only").CombinedOutput(); err != nil {
					if ctx.Err() == context.Canceled {
						wg.Done()
						return
					}
					log.Println("could not pull", URL, folder, err, ctx.Err(), string(b))
					continue
				}
				for image, cfg := range images {
					if URL == cfg.URL {
						path := filepath.Join(folder, cfg.path)
						file := filepath.Join(path, cfg.file)
						log.Println("building", image)
						if b, err := exec.CommandContext(ctx, "docker", "build", "--tag", image, "--file", file, path).CombinedOutput(); err != nil {
							if ctx.Err() == context.Canceled {
								wg.Done()
								return
							}
							log.Println("could not build", URL, image, folder, path, file, err, ctx.Err(), string(b))
						} else {
							log.Println("building", image, "done")
						}
					}
				}
			}
		}()
	}
	wg.Wait()
	done <- struct{}{}
}

func main() {
	file, err := os.OpenFile("log.txt", os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_SYNC, 0644)
	if err != nil {
		panic(err)
	}
	defer file.Close()
	// Redirect stderr (default log & panic output) to log.txt
	if err := syscall.Dup2(int(file.Fd()), int(os.Stderr.Fd())); err != nil {
		panic(err)
	}
	if err := os.Mkdir("repositories", os.ModePerm); err != nil && !errors.Is(err, os.ErrExist) {
		panic(err)
	}
	if err := os.Chdir("repositories"); err != nil {
		panic(err)
	}
	for _, cfg := range images {
		updateNeeded[cfg.URL] = make(chan struct{}, 1)
		updateNeeded[cfg.URL] <- struct{}{}
	}
	buildDone := make(chan struct{})
	ctxBuild, cancelBuild := context.WithCancel(context.Background())
	go build(ctxBuild, buildDone)
	srv := http.Server{
		Addr:         ":8081",
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}
	serverDone := make(chan struct{})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Repository struct {
				URL string `json:"ssh_url"`
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			log.Println(err)
		} else if c, ok := updateNeeded[payload.Repository.URL]; ok {
			select {
			case c <- struct{}{}:
			default:
			}
		} else if payload.Repository.URL == "git@github.com:01-edu/docker.01-edu.org.git" {
			go func() {
				log.Println("shutting down")
				cancelBuild()
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if err := srv.Shutdown(ctx); err != nil {
					panic(err)
				}
				serverDone <- struct{}{}
			}()
		}
	})
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		panic(err)
	}
	if err := os.Chdir(".."); err != nil {
		panic(err)
	}
	if b, err := exec.Command("git", "pull", "--ff-only").CombinedOutput(); err != nil {
		os.Stderr.Write(b)
	}
	if b, err := exec.Command("go", "build", "-o", "main.exe", ".").CombinedOutput(); err != nil {
		os.Stderr.Write(b)
	}
	<-serverDone
	<-buildDone
	log.Println("rebooting")
	file.Close()
	if err := syscall.Exec("main.exe", []string{"main.exe"}, os.Environ()); err != nil {
		panic(err)
	}
}
