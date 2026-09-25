package protocolgen

// typeSelection owns naming and selection only for one construction. It is never
// retained on the mutable input plan or shared between generation calls.
type typeSelection struct {
	plan                                    ProtocolTypePlan
	resolver                                generatedDefinitionNameResolver
	selectGeneratedEnums                    []EnumPlan
	selectGeneratedEnumsDone                bool
	selectGeneratedEnumsErr                 error
	selectGeneratedScalarAliases            []ScalarAliasPlan
	selectGeneratedScalarAliasesDone        bool
	selectGeneratedScalarAliasesErr         error
	selectFirstPassGeneratedTypes           []TypePlan
	selectFirstPassGeneratedTypesDone       bool
	selectFirstPassGeneratedTypesErr        error
	selectGeneratedMixedUnions              []MixedUnionPlan
	selectGeneratedMixedUnionsDone          bool
	selectGeneratedMixedUnionsErr           error
	selectGeneratedUntaggedObjectUnions     []UntaggedObjectUnionPlan
	selectGeneratedUntaggedObjectUnionsDone bool
	selectGeneratedUntaggedObjectUnionsErr  error
	selectGeneratedScalarUnions             []ScalarUnionPlan
	selectGeneratedScalarUnionsDone         bool
	selectGeneratedScalarUnionsErr          error
	selectGeneratedTaggedUnions             []TaggedUnionPlan
	selectGeneratedTaggedUnionsDone         bool
	selectGeneratedTaggedUnionsErr          error
}

func newTypeSelection(plan ProtocolTypePlan) (*typeSelection, error) {
	plan = normalizeExplicitProtocolTypePlan(plan)
	resolver, err := newGeneratedDefinitionNameResolver(plan)
	if err != nil {
		return nil, err
	}
	return &typeSelection{plan: plan, resolver: resolver}, nil
}

func SelectGeneratedEnums(plan ProtocolTypePlan) ([]EnumPlan, error) {
	c, err := newTypeSelection(plan)
	if err != nil {
		return nil, err
	}
	return c.SelectGeneratedEnums()
}

func SelectGeneratedScalarAliases(plan ProtocolTypePlan) ([]ScalarAliasPlan, error) {
	c, err := newTypeSelection(plan)
	if err != nil {
		return nil, err
	}
	return c.SelectGeneratedScalarAliases()
}

func SelectFirstPassGeneratedTypes(plan ProtocolTypePlan) ([]TypePlan, error) {
	c, err := newTypeSelection(plan)
	if err != nil {
		return nil, err
	}
	return c.SelectFirstPassGeneratedTypes()
}

func SelectGeneratedMixedUnions(plan ProtocolTypePlan) ([]MixedUnionPlan, error) {
	c, err := newTypeSelection(plan)
	if err != nil {
		return nil, err
	}
	return c.SelectGeneratedMixedUnions()
}

func SelectGeneratedUntaggedObjectUnions(plan ProtocolTypePlan) ([]UntaggedObjectUnionPlan, error) {
	c, err := newTypeSelection(plan)
	if err != nil {
		return nil, err
	}
	return c.SelectGeneratedUntaggedObjectUnions()
}

func SelectGeneratedScalarUnions(plan ProtocolTypePlan) ([]ScalarUnionPlan, error) {
	c, err := newTypeSelection(plan)
	if err != nil {
		return nil, err
	}
	return c.SelectGeneratedScalarUnions()
}

func SelectGeneratedTaggedUnions(plan ProtocolTypePlan) ([]TaggedUnionPlan, error) {
	c, err := newTypeSelection(plan)
	if err != nil {
		return nil, err
	}
	return c.SelectGeneratedTaggedUnions()
}
