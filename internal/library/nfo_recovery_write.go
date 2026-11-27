package library

import "errors"

// A rename was attempted. Callers must re-observe the leaf rather than assume
// it still contains the damaged bytes or retry this stage.
var errNFORecoveryPublicationUncertain = errors.New("NFO recovery publication is uncertain")
