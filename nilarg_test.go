package nilarg_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
	"github.com/Matts966/nilarg"
)

func Test(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, nilarg.Analyzer, "a")
}
