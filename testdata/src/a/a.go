package a

type X struct{ f, g int }

func f(i int, ip *int, x X, xp *X) {
	print(i, *ip, x, *xp)
}

func f2(ptr *[3]int, i interface{}) {
	if ptr != nil {
		print(ptr[:])
		*ptr = [3]int{}
		print(*ptr)
	} else {
		print(ptr[:])
		*ptr = [3]int{}
		print(*ptr)

		if ptr != nil {
			print(*ptr)
		}
	}
	if i != nil {
		print(i.(interface{ f() }))
	} else {
		print(i.(interface{ f() }))
	}
}

func f3(x *int) {
	if x != nil {
		_ = *x
	}
}
