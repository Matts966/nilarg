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

// panicArgs has the information about arguments which causes panic on
// calling the function when it is nil.
type panicArgs map[int]struct{}

func (*panicArgs) AFact() {}

func run(pass *analysis.Pass) (interface{}, error) {
	ssainput := pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA)
	for _, fn := range ssainput.SrcFuncs {
		checkFunc(pass, fn)
	}
	return nil, nil
}

// checkFunc finds arguments of fn that can be nil and cause panic in
// fn when they are nil and export their information as the Facts.
//
// The Facts are such as:
// 	f(x *int) { *x }
// and:
// 	f(m map[int]int) { map[5] = 5 }
// and:
// 	f(i interface{}) { i.(interface{ f() }) }
//
// These codes do not always cause panic, but panic if the argument is nil.
func checkFunc(pass *analysis.Pass, fn *ssa.Function) {
	fact := panicArgs{}
	for i, fp := range fn.Params {
		// If the argument fp can't be nil or there are no referrers
		// of fp in fn, skip check.
		if !isNillable(fp.Type()) {
			continue
		}
		if fp.Referrers() == nil {
			continue
		}

	refLoop:
		// Check all the referrers and if the instruction cause panic when
		// fp is nil, add fact of it and break this loop.
		for _, fpr := range *fp.Referrers() {
			switch instr := fpr.(type) {
			case *ssa.FieldAddr:
				// the address of fp.field
				if instr.X == fp && !isNilChecked(fp, instr.Block(), nil) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.Field:
				// fp.field
				if instr.X == fp && !isNilChecked(fp, instr.Block(), nil) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.IndexAddr:
				// fp[i]
				if instr.X == fp && !isNilChecked(fp, instr.Block(), nil) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.TypeAssert:
				// fp.(someType)
				if instr.X == fp && !isNilChecked(fp, instr.Block(), nil) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.Slice:
				// Slice operation to a pointer fp cause nil pointer
				// dereference iff fp is nil.
				//
				// fp[:]
				if _, ok := instr.X.Type().Underlying().(*types.Pointer); ok && instr.X == fp && !isNilChecked(fp, instr.Block(), nil) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.Store:
				// *fp = v
				if instr.Addr == fp && !isNilChecked(fp, instr.Block(), nil) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.MapUpdate:
				// *fp[x] = y
				if instr.Map == fp && !isNilChecked(fp, instr.Block(), nil) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.UnOp:
				// *fp
				if instr.X == fp && instr.Op == token.MUL && !isNilChecked(fp, instr.Block(), nil) {
					fact[i] = struct{}{}
					break refLoop
				}
			}
		}
	}
	// If no argument cause panic, skip exporting the fact.
	if len(fact) > 0 && fn.Object() != nil {
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

// isNilChecked returns true when the v is already nil checked and
// definitely not nil in the b.
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
