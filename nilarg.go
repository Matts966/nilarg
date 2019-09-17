package nilarg

import (
	"go/token"
	"go/types"
	"reflect"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
)

const Doc = `check if the function will panic on nil arguments
`

var Analyzer = &analysis.Analyzer{
	Name:       "nilarg",
	Doc:        Doc,
	Run:        run,
	Requires:   []*analysis.Analyzer{buildssa.Analyzer},
	ResultType: reflect.TypeOf(PanicArgs{}),
}

var ssapkgs = make(map[string]*ssa.Package)

// PanicArgs has the information about arguments which causes panic on calling the function when it is nil.
type PanicArgs map[string]map[int]struct{}

func run(pass *analysis.Pass) (interface{}, error) {
	pa := PanicArgs{}
	ssainput := pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA)
	for _, fn := range ssainput.SrcFuncs {
		runFunc(pass, fn, pa, true)
	}
	return pa, nil

}

func runFunc(pass *analysis.Pass, fn *ssa.Function, pa PanicArgs, src bool) {
	if _, ok := pa[fn.Object().(*types.Func).FullName()]; ok {
		return
	}
	for i, fp := range fn.Params {
		if !isNillable(fp.Type()) {
			continue
		}
		if fp.Referrers() == nil {
			continue
		}

		for _, fpr := range *fp.Referrers() {
			switch instr := fpr.(type) {
			case *ssa.FieldAddr:
				if instr.X == fp && !isNilChecked(fp, instr.Block(), []*ssa.BasicBlock{}) {
					appendPanicArg(pa, fn, i)
					break
				}
			case *ssa.IndexAddr:
				if instr.X == fp && !isNilChecked(fp, instr.Block(), []*ssa.BasicBlock{}) {
					appendPanicArg(pa, fn, i)
					break
				}
			case *ssa.TypeAssert:
				if instr.X == fp && !isNilChecked(fp, instr.Block(), []*ssa.BasicBlock{}) {
					appendPanicArg(pa, fn, i)
					break
				}
			case *ssa.Slice:
				if _, ok := instr.X.Type().Underlying().(*types.Pointer); ok && instr.X == fp && !isNilChecked(fp, instr.Block(), []*ssa.BasicBlock{}) {
					appendPanicArg(pa, fn, i)
					break
				}
			case *ssa.Store:
				if instr.Addr == fp && !isNilChecked(fp, instr.Block(), []*ssa.BasicBlock{}) {
					appendPanicArg(pa, fn, i)
					break
				}
			case *ssa.MapUpdate:
				if instr.Map == fp && !isNilChecked(fp, instr.Block(), []*ssa.BasicBlock{}) {
					appendPanicArg(pa, fn, i)
					break
				}
			case *ssa.UnOp:
				if instr.X == fp && instr.Op == token.MUL && !isNilChecked(fp, instr.Block(), []*ssa.BasicBlock{}) {
					appendPanicArg(pa, fn, i)
					break
				}
			}
		}
	}

	if !src {
		return
	}

	for _, b := range fn.Blocks {
		for _, i := range b.Instrs {
			c, ok := i.(ssa.CallInstruction)
			if !ok {
				continue
			}
			checkCall(pass, c, fn, pa)
		}
	}
}

func checkCall(pass *analysis.Pass, c ssa.CallInstruction, fn *ssa.Function, pa PanicArgs) {
	var sf *ssa.Function
	if c.Common().IsInvoke() {
		sf = fn.Prog.FuncValue(c.Common().Method)
	} else {
		sf = c.Common().StaticCallee()
	}

	if sf == nil {
		return
	}
	if _, ok := pa[sf.Object().(*types.Func).FullName()]; ok {
		return
	}

	if ssapkg, ok := ssapkgs[sf.Pkg.Pkg.Name()]; ok {
		if f := ssapkg.Func(sf.Name()); f != nil {
			runFunc(pass, f, pa, false)
		}
		for _, m := range ssapkg.Members {
			mt, ok := m.(*ssa.Type)
			if !ok {
				continue
			}
			runMethod(pass, pa, ssapkg, mt.Type(), sf.Name())
		}
		return
	}

	cfg := &packages.Config{}
	cfg.Mode = packages.LoadAllSyntax
	// cfg.Tests = true
	pkgs, _ := packages.Load(cfg, sf.Pkg.Pkg.Name())
	for _, pkg := range pkgs {
		prog := ssa.NewProgram(pkg.Fset, 0)
		created := make(map[*types.Package]bool)
		var createAll func(pkgs []*types.Package)
		createAll = func(pkgs []*types.Package) {
			for _, p := range pkgs {
				if !created[p] {
					created[p] = true
					prog.CreatePackage(p, nil, nil, true)
					createAll(p.Imports())
				}
			}
		}
		createAll(pkg.Types.Imports())
		ssapkg := prog.CreatePackage(pkg.Types, pkg.Syntax, pkg.TypesInfo, true)
		ssapkg.Build()
		ssapkgs[sf.Pkg.Pkg.Name()] = ssapkg
		if f := ssapkg.Func(sf.Name()); f != nil {
			runFunc(pass, f, pa, false)
			break
		}
		for _, m := range ssapkg.Members {
			mt, ok := m.(*ssa.Type)
			if !ok {
				continue
			}
			runMethod(pass, pa, ssapkg, mt.Type(), sf.Name())
		}
		continue
	}
}

func runMethod(pass *analysis.Pass, pa PanicArgs, ssapkg *ssa.Package, t types.Type, name string) {
	_, index, indirect := types.LookupFieldOrMethod(t, false, ssapkg.Pkg, name)
	if index == nil && !indirect {
		return
	}
	if indirect {
		if f := ssapkg.Prog.LookupMethod(types.NewPointer(t), ssapkg.Pkg, name); f != nil {
			runFunc(pass, f, pa, false)
		}
		return
	}
	if f := ssapkg.Prog.LookupMethod(t, ssapkg.Pkg, name); f != nil {
		runFunc(pass, f, pa, false)
	}
}

func appendPanicArg(pa PanicArgs, fn *ssa.Function, i int) {
	ff := fn.Object().(*types.Func).FullName()
	if _, ok := pa[ff]; ok {
		pa[ff][i] = struct{}{}
		return
	}
	pa[ff] = make(map[int]struct{})
	pa[ff][i] = struct{}{}
}

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

func isNil(v ssa.Value) bool {
	if v, ok := v.(*ssa.Const); ok {
		return v.IsNil()
	}
	return false
}
