package application

import (
	"errors"

	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
)

func preserveCatalogSourceError(message string, err error) error {
	var domainError *contracts.DomainError
	if errors.As(err, &domainError) {
		return err
	}
	return contracts.NewDomainError(contracts.ErrorCodeUpstreamUnavailable, message, true, err)
}

func errorCodeOr(err error, fallback contracts.ErrorCode) contracts.ErrorCode {
	var domainError *contracts.DomainError
	if errors.As(err, &domainError) {
		return domainError.Code
	}
	return fallback
}

func isRetryableError(err error) bool {
	var domainError *contracts.DomainError
	return errors.As(err, &domainError) && domainError.Retryable
}

func invalidManifest(message string, cause error) error {
	return contracts.NewDomainError(contracts.ErrorCodeInvalidManifest, message, false, cause)
}
