package auth

import (
	"fmt"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
)

// PermissionError is returned by permission check functions when access is denied.
// It carries the HTTP status and error code so the caller can respond correctly.
type PermissionError struct {
	Status  int
	Code    apperrors.ErrorCode
	Message string
}

func (e *PermissionError) Error() string {
	return e.Message
}

// checkIsnAccess validates that the account has access to the ISN and that the ISN is in use.
// Returns the ISN permissions on success, or a *PermissionError on failure.
func checkIsnAccess(claims *Claims, isnSlug string) (IsnPerm, error) {
	perms, ok := claims.IsnPerms[isnSlug]
	if !ok {
		return IsnPerm{}, &PermissionError{
			Status:  http.StatusForbidden,
			Code:    apperrors.ErrCodeForbidden,
			Message: fmt.Sprintf("account does not have access to ISN %q", isnSlug),
		}
	}
	if !perms.InUse {
		return IsnPerm{}, &PermissionError{
			Status:  http.StatusNotFound,
			Code:    apperrors.ErrCodeResourceNotFound,
			Message: fmt.Sprintf("ISN not in use (%s)", isnSlug),
		}
	}
	return perms, nil
}

// checkSignalType validates that the signal type exists on the ISN and is in use.
// A no-op when signalTypePath is empty.
func checkSignalType(perms IsnPerm, isnSlug, signalTypePath string) error {
	if signalTypePath == "" {
		return nil
	}
	signalType, found := perms.SignalTypes[signalTypePath]
	if !found {
		return &PermissionError{
			Status:  http.StatusNotFound,
			Code:    apperrors.ErrCodeResourceNotFound,
			Message: fmt.Sprintf("signal type %q not found on ISN %q", signalTypePath, isnSlug),
		}
	}
	if !signalType.InUse {
		return &PermissionError{
			Status:  http.StatusNotFound,
			Code:    apperrors.ErrCodeResourceNotFound,
			Message: fmt.Sprintf("signal type not in use on ISN (%s: %s)", signalTypePath, isnSlug),
		}
	}
	return nil
}

// CheckIsnReadPermission checks that the claims grant read access to the given ISN
// and that the ISN is in use. If signalTypePath is non-empty it also checks the signal
// type exists and is in use on that ISN.
func CheckIsnReadPermission(claims *Claims, isnSlug, signalTypePath string) error {
	perms, err := checkIsnAccess(claims, isnSlug)
	if err != nil {
		return err
	}
	if !perms.CanRead {
		return &PermissionError{
			Status:  http.StatusForbidden,
			Code:    apperrors.ErrCodeForbidden,
			Message: fmt.Sprintf("account does not have read permission on ISN %q", isnSlug),
		}
	}
	return checkSignalType(perms, isnSlug, signalTypePath)
}

// CheckIsnWritePermission checks that the claims grant write access to the given ISN
// and that the ISN is in use. If signalTypePath is non-empty it also checks the signal
// type exists and is in use on that ISN.
func CheckIsnWritePermission(claims *Claims, isnSlug, signalTypePath string) error {
	perms, err := checkIsnAccess(claims, isnSlug)
	if err != nil {
		return err
	}
	if !perms.CanWrite {
		return &PermissionError{
			Status:  http.StatusForbidden,
			Code:    apperrors.ErrCodeForbidden,
			Message: fmt.Sprintf("account does not have write permission on ISN %q", isnSlug),
		}
	}
	return checkSignalType(perms, isnSlug, signalTypePath)
}
