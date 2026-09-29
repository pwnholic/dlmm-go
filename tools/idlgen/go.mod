// Separate module so the generator's dependencies never enter the SDK's
// go.mod. The generator uses only the standard library, and keeping it out of
// the main module is what stops a future codegen dependency from reaching
// consumers of the library.
module github.com/pwnholic/dlmm-go/tools/idlgen

go 1.25.0
