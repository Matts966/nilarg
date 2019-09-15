package main

import (
	"github.com/Matts966/nilarg"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() { singlechecker.Main(nilarg.Analyzer) }
