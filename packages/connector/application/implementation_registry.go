package application

import (
	"fmt"

	"github.com/tutti-os/tutti/packages/connector/contracts"
)

type ImplementationValidator func(contracts.Implementation) error

type ImplementationRegistry struct {
	validators map[string]ImplementationValidator
}

func NewImplementationRegistry(validators map[string]ImplementationValidator) ImplementationRegistry {
	cloned := make(map[string]ImplementationValidator, len(validators))
	for kind, validator := range validators {
		cloned[kind] = validator
	}
	return ImplementationRegistry{validators: cloned}
}

func (registry ImplementationRegistry) Supports(kind string) bool {
	_, ok := registry.validators[kind]
	return ok
}

func (registry ImplementationRegistry) Validate(manifest contracts.Manifest) error {
	if err := contracts.ValidateManifestShape(manifest); err != nil {
		return err
	}
	validator, ok := registry.validators[manifest.Implementation.Kind]
	if !ok {
		return contracts.NewDomainError(
			contracts.ErrorCodeUnsupportedImplementation,
			fmt.Sprintf("implementation %q is not supported", manifest.Implementation.Kind),
			false,
			nil,
		)
	}
	if validator != nil {
		if err := validator(manifest.Implementation); err != nil {
			return contracts.NewDomainError(contracts.ErrorCodeInvalidManifest, "implementation is invalid", false, err)
		}
	}
	return nil
}
