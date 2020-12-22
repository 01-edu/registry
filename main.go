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

var (
	imagesToBuild = map[string]config{
		"lib-go":     {"git@github.com:01-edu/all.git", "lib/go", "Dockerfile"},
		"lib-js":     {"git@github.com:01-edu/all.git", "lib/js", "Dockerfile"},
		"lib-static": {"git@github.com:01-edu/all.git", "static", "Dockerfile.lib"},
		"test-dom":   {"git@github.com:01-edu/public.git", "", "dom/Dockerfile"},
		"test-go":    {"git@github.com:01-edu/public.git", "go/tests", "Dockerfile"},
		"test-js":    {"git@github.com:01-edu/public.git", "js/tests", "Dockerfile"},
		"test-rust":  {"git@github.com:01-edu/test-rust.git", "", "Dockerfile"},
		"test-sh":    {"git@github.com:01-edu/public.git", "sh/tests", "Dockerfile"},
		"subjects":   {"git@github.com:01-edu/public.git", "subjects", "Dockerfile"},
	}

	imagesToMirror = map[string]struct{}{
		"alpine:3.12.0":                               {},
		"alpine/git:1.0.20":                           {},
		"ankane/pghero:v2.7.0":                        {},
		"caddy:2.1.1-alpine":                          {},
		"gitea/gitea:1.11.8":                          {},
		"golang:1.14.6-alpine3.12":                    {},
		"hasura/graphql-engine:v1.3.2.cli-migrations": {},
		"node:12.18.3-alpine3.12":                     {},
		"postgres:11.8":                               {},
	}

	webhooksToCall = map[string]map[string]struct{}{
		"https://01.alem.school/api/updater":               {"test-dom": {}, "test-go": {}, "test-js": {}, "test-rust": {}, "test-sh": {}},
		"https://demo.01-edu.org/api/updater":              {"test-dom": {}, "test-go": {}, "test-js": {}, "test-rust": {}, "test-sh": {}},
		"https://honoriscentraleit.01-edu.org/api/updater": {"test-dom": {}, "test-go": {}, "test-js": {}, "test-rust": {}, "test-sh": {}},
		"https://ytrack.learn.ynov.com/api/updater":        {"test-dom": {}, "test-go": {}, "test-js": {}, "test-rust": {}, "test-sh": {}},
		"https://beta.01-edu.org/api/updater":              {"test-dom": {}, "test-go": {}, "test-js": {}, "test-rust": {}, "test-sh": {}},
	}

	// the keys are repositories URL
	buildNeeded = map[string]chan struct{}{}
)

func run(ctx context.Context, commands [][]string) error {
	for _, command := range commands {
		if b, err := exec.CommandContext(ctx, command[0], command[1:]...).CombinedOutput(); err != nil {
			if ctx.Err() != context.Canceled {
				log.Println(command, err, ctx.Err(), string(b))
			}
			return err
		}
	}
	return nil
}

func build(ctx context.Context, done chan<- struct{}) {
	var wg sync.WaitGroup
	for URL, c := range buildNeeded {
		URL := URL
		c := c
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-c:
				case <-time.After(5 * time.Minute):
				case <-ctx.Done():
					return
				}
				ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
				defer cancel()
				folder := strings.TrimSuffix(strings.TrimPrefix(URL, "git@github.com:01-edu/"), ".git")
				if _, err := os.Stat(folder); os.IsNotExist(err) {
					if err := run(ctx, [][]string{{"git", "clone", URL, folder}}); err == context.Canceled {
						return
					} else if err != nil {
						continue
					}
				}
				if err := run(ctx, [][]string{{"git", "-C", folder, "pull", "--ff-only"}}); err == context.Canceled {
					return
				} else if err != nil {
					continue
				}
				for image, cfg := range imagesToBuild {
					if URL == cfg.URL {
						path := filepath.Join(folder, cfg.path)
						file := filepath.Join(path, cfg.file)
						log.Println("building", image)
						if err := run(ctx, [][]string{
							{"docker", "build", "--tag", image, "--file", file, path},
							{"docker", "tag", image, "docker.01-edu.org/" + image},
							{"docker", "push", "docker.01-edu.org/" + image},
						}); err == context.Canceled {
							return
						} else if err != nil {
							continue
						}
						log.Println("building", image, "done")
						for webhookToCall, images := range webhooksToCall {
							if _, ok := images[image]; ok {
								req, err := http.NewRequestWithContext(ctx, "PUT", webhookToCall, nil)
								if err != nil {
									panic(err)
								}
								resp, err := http.DefaultClient.Do(req)
								if err == context.Canceled {
									return
								}
								if err != nil {
									log.Println(webhookToCall, err, ctx.Err())
								} else {
									resp.Body.Close()
								}
							}
						}
					}
				}
			}
		}()
	}
	wg.Wait()
	done <- struct{}{}
}

func mirror(ctx context.Context, done chan<- struct{}) {
	var wg sync.WaitGroup
	for image := range imagesToMirror {
		image := image
		wg.Add(1)
		go func() {
			for {
				ctxTimeout, cancel := context.WithTimeout(ctx, 10*time.Minute)
				log.Println("mirroring", image)
				if err := run(ctxTimeout, [][]string{
					{"docker", "pull", image},
					{"docker", "tag", image, "docker.01-edu.org/" + image},
					{"docker", "push", "docker.01-edu.org/" + image},
				}); err == nil {
					log.Println("mirroring", image, "done")
				}
				cancel()
				select {
				case <-time.After(time.Hour):
				case <-ctx.Done():
					wg.Done()
					return
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
	for _, cfg := range imagesToBuild {
		buildNeeded[cfg.URL] = make(chan struct{}, 1)
		buildNeeded[cfg.URL] <- struct{}{}
	}
	buildDone := make(chan struct{})
	mirrorDone := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go build(ctx, buildDone)
	go mirror(ctx, mirrorDone)
	srv := http.Server{
		Addr:         ":8081",
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}
	serverDone := make(chan struct{})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Ref        string
			Repository struct {
				URL string `json:"ssh_url"`
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			log.Println(err)
		} else if payload.Ref != "refs/heads/master" && payload.Ref != "refs/heads/main" {
			return
		} else if c, ok := buildNeeded[payload.Repository.URL]; ok {
			select {
			case c <- struct{}{}:
			default:
			}
		} else if payload.Repository.URL == "git@github.com:01-edu/docker.01-edu.org.git" {
			go func() {
				log.Println("shutting down")
				cancel()
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
	<-mirrorDone
	<-buildDone
	log.Println("rebooting")
	file.Close()
	if err := syscall.Exec("main.exe", []string{"main.exe"}, os.Environ()); err != nil {
		panic(err)
	}
}
