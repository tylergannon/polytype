package entrypoints

type MethodType struct {
	Name string `json:"name"`
}

type FuncType struct {
	Name string `json:"name"`
}

type BuilderType struct {
	Name string `json:"name"`
}

// PointerFuncType is a named pointer type. Go forbids declaring a method on
// it (invalid receiver base type), so its schema entrypoint must be
// registered as a free function.
type PointerFuncType *int

// InterfaceFuncType is a registered sealed interface. Like a named pointer
// type, Go forbids declaring a method on it, so its schema entrypoint must
// also be registered as a free function.
type InterfaceFuncType interface{ interfaceFuncType() }

type InterfaceFuncImpl struct {
	Name string `json:"name"`
}

func (InterfaceFuncImpl) interfaceFuncType() {}
