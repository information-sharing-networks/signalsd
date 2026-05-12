package auth

// manage isn permission checks

import (
	"fmt"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
)

// checkIsnAccess validates that the account has access to the ISN and that the ISN is in use.
func checkIsnAccess(claims *Claims, isnSlug string) (IsnPerm, error) {
	perms, ok := claims.IsnPerms[isnSlug]
	if !ok {
		return IsnPerm{}, apperrors.Forbidden(fmt.Sprintf("account does not have access to ISN %q", isnSlug), nil)
	}
	if !perms.InUse {
		return IsnPerm{}, apperrors.NotFound(fmt.Sprintf("ISN not in use (%s)", isnSlug), nil)
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
		return apperrors.NotFound(fmt.Sprintf("signal type %q not found on ISN %q", signalTypePath, isnSlug), nil)
	}
	if !signalType.InUse {
		return apperrors.NotFound(fmt.Sprintf("signal type not in use on ISN (%s: %s)", signalTypePath, isnSlug), nil)
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
		return apperrors.Forbidden(fmt.Sprintf("account does not have read permission on ISN %q", isnSlug), nil)
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
		return apperrors.Forbidden(fmt.Sprintf("account does not have write permission on ISN %q", isnSlug), nil)
	}
	return checkSignalType(perms, isnSlug, signalTypePath)
}
