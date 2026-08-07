package integration

// Source identifies which configuration layer supplies an effective value.
type Source string

const (
	SourceDefault     Source = "default"
	SourceAdmin       Source = "admin"
	SourceEnvironment Source = "environment"
)

// Field carries an effective typed value plus the precedence metadata needed
// by callers that display or edit it.
type Field[T any] struct {
	Value            T
	Source           Source
	Default          T
	HasAdminFallback bool
}

// SecretField exposes configuration metadata without exposing secret material.
type SecretField struct {
	Configured       bool
	Source           Source
	HasAdminFallback bool
}

// ResolveField applies environment, Admin, then built-in default precedence.
func ResolveField[T any](environment, admin *T, defaultValue T) Field[T] {
	source, hasAdminFallback := resolveSource(environment != nil, admin != nil)
	field := Field[T]{
		Value:            defaultValue,
		Source:           source,
		Default:          defaultValue,
		HasAdminFallback: hasAdminFallback,
	}
	switch source {
	case SourceAdmin:
		field.Value = *admin
	case SourceEnvironment:
		field.Value = *environment
	}
	return field
}

// ResolveSecretField applies the same precedence using presence flags only.
func ResolveSecretField(environmentConfigured, adminConfigured bool) SecretField {
	source, hasAdminFallback := resolveSource(environmentConfigured, adminConfigured)
	return SecretField{
		Configured:       environmentConfigured || adminConfigured,
		Source:           source,
		HasAdminFallback: hasAdminFallback,
	}
}

func resolveSource(environment, admin bool) (Source, bool) {
	if environment {
		return SourceEnvironment, admin
	}
	if admin {
		return SourceAdmin, false
	}
	return SourceDefault, false
}
