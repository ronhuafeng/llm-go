package protocolgen

// ProtocolPackage carries the emitted files and the facts used to emit them.
// Consumers do not rediscover protocol prerequisites from generated Go text.
type ProtocolPackage struct {
	MethodRegistry      []byte
	ProtocolTypes       []byte
	ExperimentalMembers []byte
	TypeNames           map[string]bool
	MethodConstants     map[string]string
}

// BuildProtocolPackage is the shared construction path for generation entrypoints.
func BuildProtocolPackage(plan ProtocolTypePlan, manifest Manifest, handwrittenDir string) (ProtocolPackage, error) {
	if err := ApplyWireMessageRoles(&plan, manifest); err != nil {
		return ProtocolPackage{}, err
	}
	selection, err := newTypeSelection(plan)
	if err != nil {
		return ProtocolPackage{}, err
	}
	types, err := selection.generateProtocolTypes()
	if err != nil {
		return ProtocolPackage{}, err
	}
	methods, err := GenerateMethodRegistry(manifest)
	if err != nil {
		return ProtocolPackage{}, err
	}
	experimental, err := GenerateExperimentalMembers(manifest)
	if err != nil {
		return ProtocolPackage{}, err
	}
	result := ProtocolPackage{ProtocolTypes: types, MethodRegistry: methods, ExperimentalMembers: experimental, TypeNames: map[string]bool{}, MethodConstants: map[string]string{}}
	knownSources := map[string][]string{}
	for _, item := range selection.selectGeneratedEnums {
		result.TypeNames[item.TypeName] = true
		knownSources["protocol_types.gen.go:type:"+item.TypeName] = append(knownSources["protocol_types.gen.go:type:"+item.TypeName], item.Sources...)
	}
	for _, item := range selection.selectGeneratedScalarAliases {
		result.TypeNames[item.TypeName] = true
		knownSources["protocol_types.gen.go:type:"+item.TypeName] = append(knownSources["protocol_types.gen.go:type:"+item.TypeName], item.Sources...)
	}
	for _, item := range selection.selectFirstPassGeneratedTypes {
		result.TypeNames[item.TypeName] = true
		knownSources["protocol_types.gen.go:type:"+item.TypeName] = append(knownSources["protocol_types.gen.go:type:"+item.TypeName], item.SchemaPath)
	}
	for _, item := range selection.selectGeneratedScalarUnions {
		result.TypeNames[item.TypeName] = true
		knownSources["protocol_types.gen.go:type:"+item.TypeName] = append(knownSources["protocol_types.gen.go:type:"+item.TypeName], item.SchemaPath)
	}
	for _, item := range selection.selectGeneratedMixedUnions {
		result.TypeNames[item.TypeName] = true
		knownSources["protocol_types.gen.go:type:"+item.TypeName] = append(knownSources["protocol_types.gen.go:type:"+item.TypeName], item.SchemaPath)
	}
	for _, item := range selection.selectGeneratedUntaggedObjectUnions {
		result.TypeNames[item.TypeName] = true
		knownSources["protocol_types.gen.go:type:"+item.TypeName] = append(knownSources["protocol_types.gen.go:type:"+item.TypeName], item.SchemaPath)
	}
	for _, item := range selection.selectGeneratedTaggedUnions {
		result.TypeNames[item.TypeName] = true
		knownSources["protocol_types.gen.go:type:"+item.TypeName] = append(knownSources["protocol_types.gen.go:type:"+item.TypeName], item.SchemaPath)
	}
	for _, entry := range manifest.Entries {
		result.MethodConstants[entry.Method] = methodConstName(entry.Method)
		if entry.SourceSchema != "" {
			knownSources["method_registry.gen.go:const:"+result.MethodConstants[entry.Method]] = []string{entry.SourceSchema}
		}
	}
	if err := validateGeneratedPackage("protocolv2", handwrittenDir, result.Files(), knownSources); err != nil {
		return ProtocolPackage{}, err
	}
	return result, nil
}

func (p ProtocolPackage) Files() map[string][]byte {
	return map[string][]byte{"method_registry.gen.go": p.MethodRegistry, "protocol_types.gen.go": p.ProtocolTypes, "experimental_members.gen.go": p.ExperimentalMembers}
}
