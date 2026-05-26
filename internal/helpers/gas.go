package helpers

import "math/big"

// Per-call gas budgets sized generously against measured worst-case usage so a
// single mailbox-root recomputation under load still fits without an OOG.
const (
	GasMint            uint64 = 200_000
	GasApprove         uint64 = 200_000
	GasNativeTransfer  uint64 = 50_000
	GasBridgeERC20To   uint64 = 800_000
	GasBridgeReceive   uint64 = 1_500_000
	GasBridgeReceiveLo uint64 = 200_000 // intentionally insufficient for the OOG scenarios
)

// Shared EIP-1559 fee defaults for local-testnet (op-rbuilder).
var (
	GasTipCap = big.NewInt(1_000_000_000)
	GasFeeCap = big.NewInt(20_000_000_000)
)
