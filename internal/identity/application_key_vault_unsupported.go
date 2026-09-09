//go:build !linux

package identity

import "context"

func (v *ApplicationKeyVault) loadMasterKey(ctx context.Context, allowCreate bool) ([applicationKeyMasterSize]byte, error) {
	if err := ctx.Err(); err != nil {
		return [applicationKeyMasterSize]byte{}, err
	}
	return [applicationKeyMasterSize]byte{}, ErrApplicationKeyVaultUnsupported
}
