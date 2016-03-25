// goimporters displays an import graph of Go packages that import the specified Go package in your GOPATH workspace.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/build"
	"log"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/kisielk/gotool"
	"github.com/shurcooL/go/importgraphutil"
	"github.com/shurcooL/go/u/u3"
	"golang.org/x/tools/refactor/importgraph"
)

func init() {
	if _, err := exec.LookPath("dot"); err != nil {
		// TODO: Replace dot with an importable native Go package to get rid of this annoying external dependency.
		fmt.Fprintln(os.Stderr, "`dot` command is required (try `brew install graphviz` to install it).")
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var (
	testsFlag = flag.Bool("tests", false, "Include tests when building graph.")
)

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: goimporters [packages]")
	flag.PrintDefaults()
}

func init() { log.SetFlags(0) }

func main() {
	flag.Usage = usage
	flag.Parse()

	err := run()
	if err != nil {
		log.Fatalln(err)
	}
}

func run() error {
	importPathPatterns := flag.Args()
	if len(importPathPatterns) == 0 {
		importPathPatterns = []string{"."}
	}
	importPaths := gotool.ImportPaths(importPathPatterns)
	importPaths, err := resolveRelative(importPaths)
	if err != nil {
		return err
	}

	var forward, reverse importgraph.Graph
	var graphErrors map[string]error
	switch *testsFlag {
	case false:
		forward, reverse, graphErrors = importgraphutil.BuildNoTests(&build.Default)
	case true:
		forward, reverse, graphErrors = importgraph.Build(&build.Default)
	}
	if graphErrors != nil {
		return fmt.Errorf("importgraph.Build: %v", graphErrors)
	}

	var reachables []map[string]bool
	for _, importPath := range importPaths {
		reachables = append(reachables, reverse.Search(importPath))
	}

	isReachable := func(k, k2 string) bool {
		for _, reachable := range reachables {
			if reachable[k] && reachable[k2] {
				return true
			}
		}
		return false
	}

	renderGraph := func() ([]byte, error) {
		var in bytes.Buffer

		fmt.Fprintln(&in, `digraph "" {`)
		for _, importPath := range importPaths {
			fmt.Fprintf(&in, "	%q [shape=box, style=bold];\n", importPath)
		}
		for k, v := range forward {
			for k2 := range v {
				if !isReachable(k, k2) {
					continue
				}

				fmt.Fprintf(&in, "	%q -> %q;\n", k, k2)
			}
		}
		in.WriteString("}")

		cmd := exec.Command("dot", "-Tsvg")
		cmd.Stdin = &in
		return cmd.Output()
	}

	graphSvg, err := renderGraph()
	if err != nil {
		return fmt.Errorf("renderGraph: %v", err)
	}

	mux := http.NewServeMux()
	stopServerChan := make(chan struct{})
	mux.HandleFunc("/index.svg", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write(graphSvg)

		go func() {
			time.Sleep(time.Second)
			stopServerChan <- struct{}{}
		}()
	})
	u3.DisplayHtmlInBrowser(mux, stopServerChan, ".svg")

	return nil
}

// resolveRelative checks that the packages exist, resolves relative import paths to full.
func resolveRelative(importPaths []string) ([]string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	for i, path := range importPaths {
		bpkg, err := build.Import(path, wd, 0)
		if err != nil {
			return nil, fmt.Errorf("can't load package %q: %v", path, err)
		}
		importPaths[i] = bpkg.ImportPath
	}
	return importPaths, nil
}
