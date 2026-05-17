package server

import (
	dsprotocol "ds2api/internal/deepseek/protocol"
)

// dsprotocolSetSalt is a thin alias so the router file doesn't need a direct
// import for a single line of init code. It also keeps the protocol package
// boundary explicit.
func dsprotocolSetSalt(salt string) {
	dsprotocol.SetFingerprintSalt(salt)
}
