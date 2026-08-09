package script

import (
	"reflect"
	"testing"
)

func TestTopLevelDeclarations(t *testing.T) {
	src := []byte(`
// a leading comment with { braces } and "quotes"
var CONFIG = {};
var STATE = { count: 0, calc: function() {
  var innerLocal = 1; // must NOT be reported: nested inside a function expression
  function innerHelper() {} // also nested
  return innerLocal;
} };
var a = 1, b = "two, three {four}", c = { x: [1,2,3] };

function topFn(x) {
  var localVar = x; // nested, must NOT be reported
  if (x) {
    var alsoNested = 2; // nested (inside if-block), must NOT be reported
  }
  return localVar;
}

function anotherFn() {}

/* a block comment with a fake declaration
var fakeVar = 1;
function fakeFn() {}
*/

var trailingAfterComment = "ok";
`)

	got := topLevelDeclarations(src)
	want := []string{
		"CONFIG", "STATE", "a", "b", "c",
		"topFn", "anotherFn", "trailingAfterComment",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("topLevelDeclarations() = %v, want %v", got, want)
	}
}

func TestTopLevelDeclarations_StringWithBraces(t *testing.T) {
	src := []byte(`var msg = "in function {foo} called from bar(1,2)";
function realFn() {}
`)
	got := topLevelDeclarations(src)
	want := []string{"msg", "realFn"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("topLevelDeclarations() = %v, want %v", got, want)
	}
}
