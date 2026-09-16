package builder

// Exports for the builder_test package. The header that opens every generated
// file and the ownership check that matches it are unexported, so the
// load-based tests in package builder_test would otherwise have to assert
// against their own copy of the header text -- a copy that could drift from
// the constant generation actually uses.

var GeneratedGoHeader = generatedGoHeader

var OwnedGeneratedFile = ownedGeneratedFile
