package fixture

// simplify_fixture.go — test fixture for ApplyCandidates.
// Contains patterns that would be flagged by simplify-detect.sh.

import "fmt"

func ExampleFunc() {
	// This is a long function with some issues.
	x := 1
	y := 2
	z := x + y
	fmt.Println(z)
}

func AnotherFunc() string {
	return "hello world"
}
