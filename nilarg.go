package nilarg

import (
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/ssa"
)

const Doc = `check if the function will panic on nil arguments
`

var Analyzer = &analysis.Analyzer{
	Name:      "nilarg",
	Doc:       Doc,
	Run:       run,
	Requires:  []*analysis.Analyzer{buildssa.Analyzer},
	FactTypes: []analysis.Fact{new(panicArgs)},
}

// ssapkgs stores compiled ssa packages for checking imported packages.
// Note that imported packages are not fully compiled because of lack of
// files by default, and need to be compiled.
var ssapkgs = make(map[string]*ssa.Package)

// panicArgs has the information about arguments which causes panic on calling the function when it is nil.
type panicArgs map[int]struct{}

func (*panicArgs) AFact() {}

func run(pass *analysis.Pass) (interface{}, error) {
	ssainput := pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA)
	for _, fn := range ssainput.SrcFuncs {
		checkFunc(pass, fn)
	}
	return nil, nil
}

// checkFunc appends the information of arguments to pa when they cause
// panic in fn. Also, if src is set true, the CallInstructions in fn
// will be checked as the same.
func checkFunc(pass *analysis.Pass, fn *ssa.Function) {
	fact := panicArgs{}
	if fn.Object() == nil {
		return
	}
	if pass.ImportObjectFact(fn.Object(), &fact) {
		return
	}
	for i, fp := range fn.Params {
		if !isNillable(fp.Type()) {
			continue
		}
		if fp.Referrers() == nil {
			continue
		}

	refLoop:
		for _, fpr := range *fp.Referrers() {
			switch instr := fpr.(type) {
			case *ssa.FieldAddr:
				if instr.X == fp && !isNilChecked(fp, instr.Block(), nil) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.Field:
				if instr.X == fp && !isNilChecked(fp, instr.Block(), nil) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.IndexAddr:
				if instr.X == fp && !isNilChecked(fp, instr.Block(), nil) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.TypeAssert:
				if instr.X == fp && !isNilChecked(fp, instr.Block(), nil) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.Slice:
				if _, ok := instr.X.Type().Underlying().(*types.Pointer); ok && instr.X == fp && !isNilChecked(fp, instr.Block(), nil) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.Store:
				if instr.Addr == fp && !isNilChecked(fp, instr.Block(), nil) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.MapUpdate:
				if instr.Map == fp && !isNilChecked(fp, instr.Block(), nil) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.UnOp:
				if instr.X == fp && instr.Op == token.MUL && !isNilChecked(fp, instr.Block(), nil) {
					fact[i] = struct{}{}
					break refLoop
				}
			}
		}
	}
	if len(fact) > 0 {
		pass.ExportObjectFact(fn.Object(), &fact)
	}
}

// isNillable returns true when the values of t can be nil.
func isNillable(t types.Type) bool {
	switch t.Underlying().(type) {
	case *types.Slice,
		*types.Interface,
		*types.Map,
		*types.Pointer:
		return true
	default:
		return false
	}
}

// isNilChecked returns true when the v is already nil checked and definitely not nil in the b.
func isNilChecked(v *ssa.Parameter, b *ssa.BasicBlock, visited []*ssa.BasicBlock) bool {
	for _, v := range visited {
		if v == b {
			return false
		}
	}
	// We could be more precise with full dataflow
	// analysis of control-flow joins.
	bi := b.Idom()
	if bi == nil {
		return false
	}
	var binop *ssa.BinOp
	// IfInstruction is unique and last instruction if any in block.
	if If, ok := bi.Instrs[len(bi.Instrs)-1].(*ssa.If); ok {
		if binop, ok = If.Cond.(*ssa.BinOp); ok {
			switch binop.Op {
			case token.EQL:
				if isNil(binop.X) && binop.Y == v || isNil(binop.Y) && binop.X == v {
					return b == bi.Succs[1]
				}
			case token.NEQ:
				if isNil(binop.X) && binop.Y == v || isNil(binop.Y) && binop.X == v {
					return b == bi.Succs[0]
				}
			}
		}
	}
	visited = append(visited, b)
	return isNilChecked(v, b, visited)
}

// isNil returns true when the value is a constant nil.
func isNil(value ssa.Value) bool {
	v, ok := value.(*ssa.Const)
	return ok && v.IsNil()
}
