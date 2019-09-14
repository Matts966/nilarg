package main

import (
	"golang.org/x/tools/go/analysis/passes/nilarg"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() { singlechecker.Main(nilarg.Analyzer) }
