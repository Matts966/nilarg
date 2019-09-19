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

// ssapkgs stores compiled ssa packages for checking imported packages.
// Note that imported packages are not fully compiled because of lack of
// files by default, and need to be compiled.
var ssapkgs = make(map[string]*ssa.Package)

// PanicArgs has the information about arguments which causes panic on calling the function when it is nil.
type PanicArgs map[string]map[int]struct{}

func run(pass *analysis.Pass) (interface{}, error) {
	pa := PanicArgs{}
	ssainput := pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA)
	for _, fn := range ssainput.SrcFuncs {
		checkFunc(fn, pa, true)
	}
	return pa, nil
}

// checkFunc appends the information of arguments to pa when they cause
// panic in fn. Also, if src is set true, the CallInstructions in fn
// will be checked as the same.
func checkFunc(fn *ssa.Function, pa PanicArgs, src bool) {
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

	refLoop:
		for _, fpr := range *fp.Referrers() {
			switch instr := fpr.(type) {
			case *ssa.FieldAddr:
				if instr.X == fp && !isNilChecked(fp, instr.Block(), nil) {
					appendPanicArg(pa, fn, i)
					break refLoop
				}
			case *ssa.Field:
				if instr.X == fp && !isNilChecked(fp, instr.Block(), nil) {
					appendPanicArg(pa, fn, i)
					break refLoop
				}
			case *ssa.IndexAddr:
				if instr.X == fp && !isNilChecked(fp, instr.Block(), nil) {
					appendPanicArg(pa, fn, i)
					break refLoop
				}
			case *ssa.TypeAssert:
				if instr.X == fp && !isNilChecked(fp, instr.Block(), nil) {
					appendPanicArg(pa, fn, i)
					break refLoop
				}
			case *ssa.Slice:
				if _, ok := instr.X.Type().Underlying().(*types.Pointer); ok && instr.X == fp && !isNilChecked(fp, instr.Block(), nil) {
					appendPanicArg(pa, fn, i)
					break refLoop
				}
			case *ssa.Store:
				if instr.Addr == fp && !isNilChecked(fp, instr.Block(), nil) {
					appendPanicArg(pa, fn, i)
					break refLoop
				}
			case *ssa.MapUpdate:
				if instr.Map == fp && !isNilChecked(fp, instr.Block(), nil) {
					appendPanicArg(pa, fn, i)
					break refLoop
				}
			case *ssa.UnOp:
				if instr.X == fp && instr.Op == token.MUL && !isNilChecked(fp, instr.Block(), nil) {
					appendPanicArg(pa, fn, i)
					break refLoop
				}
			}
		}
	}

	if !src {
		return
	}

	// Check function calls in SrcFuncs.
	for _, b := range fn.Blocks {
		for _, i := range b.Instrs {
			c, ok := i.(ssa.CallInstruction)
			if !ok {
				continue
			}
			checkCall(c, pa)
		}
	}
}

// checkCall checks a ssa.CallInstruction and appends the information to pa
// if called function panics on nil arguments.
// This check is executed only when the function is in the imported package.
func checkCall(c ssa.CallInstruction, pa PanicArgs) {
	var sf *ssa.Function
	if c.Common().IsInvoke() {
		sf = c.Parent().Prog.FuncValue(c.Common().Method)
	} else {
		sf = c.Common().StaticCallee()
	}

	if sf == nil {
		return
	}
	if _, ok := pa[sf.Object().(*types.Func).FullName()]; ok {
		return
	}
	if sf.Pkg == c.Parent().Pkg {
		return
	}

	path := sf.Pkg.Pkg.Path()

	check := func(ssapkg *ssa.Package) {
		if f := ssapkg.Func(sf.Name()); f != nil {
			checkFunc(f, pa, false)
			return
		}
		for _, m := range ssapkg.Members {
			mt, ok := m.(*ssa.Type)
			if !ok {
				continue
			}
			t := mt.Type()
			if c.Common().IsInvoke() {
				if !types.Implements(t, ssapkg.Pkg.Scope().Lookup(c.Common().Value.Type().String()).Type().(*types.Interface)) {
					continue
				}
				checkMethod(pa, ssapkg, t, sf.Name())
			} else {
				if len(c.Common().Args) < 1 {
					continue
				}
				recv := c.Common().Args[0].Type()
				if t.String() == recv.String() {
					checkMethod(pa, ssapkg, t, sf.Name())
					continue
				}
				p := types.NewPointer(t)
				if p.String() == recv.String() {
					checkMethod(pa, ssapkg, p, sf.Name())
				}
			}
		}
		return
	}

	if ssapkg, ok := ssapkgs[path]; ok {
		check(ssapkg)
		return
	}

	check(buildSSA(path))
}

// checkMethod finds the method of t whose name is the name and apply checkFunc to it.
func checkMethod(pa PanicArgs, ssapkg *ssa.Package, t types.Type, name string) {
	_, index, indirect := types.LookupFieldOrMethod(t, false, ssapkg.Pkg, name)
	if index == nil && !indirect {
		return
	}
	if indirect {
		if _, ok := t.(*types.Pointer); !ok {
			t = types.NewPointer(t)
		}
		if f := ssapkg.Prog.LookupMethod(t, ssapkg.Pkg, name); f != nil {
			checkFunc(f, pa, false)
		}
		return
	}
	if f := ssapkg.Prog.LookupMethod(t, ssapkg.Pkg, name); f != nil {
		checkFunc(f, pa, false)
	}
}

// buildSSA builds SSA from the path. The path should suggest only one package.
func buildSSA(path string) *ssa.Package {
	// Build imported package.
	cfg := &packages.Config{}
	cfg.Mode = packages.LoadSyntax
	pkgs, _ := packages.Load(cfg, path)
	pkg := pkgs[0]
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
	ssapkg := prog.CreatePackage(pkg.Types, pkg.Syntax, pkg.TypesInfo, false)
	ssapkg.Build()
	ssapkgs[path] = ssapkg
	return ssapkg
}

// appendPanicArg appends the information of arguments that cause panic on fn to pa.
func appendPanicArg(pa PanicArgs, fn *ssa.Function, i int) {
	ff := fn.Object().(*types.Func).FullName()
	if _, ok := pa[ff]; ok {
		pa[ff][i] = struct{}{}
		return
	}
	pa[ff] = make(map[int]struct{})
	pa[ff][i] = struct{}{}
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
